package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-playground/validator/v10"
	"github.com/redis/go-redis/v9"
	"golang.org/x/time/rate"

	"pdf-html-service/internal/config"
	"pdf-html-service/internal/gotenberg"
	"pdf-html-service/internal/handlers"
	"pdf-html-service/internal/jobstore"
	appmiddleware "pdf-html-service/internal/middleware"
	"pdf-html-service/internal/pdfjobs"
	"pdf-html-service/internal/security"
	"pdf-html-service/internal/storage"
)

const (
	httpClientTimeout       = 90 * time.Second
	httpDialTimeout         = 5 * time.Second
	httpKeepAlive           = 30 * time.Second
	httpTLSHandshakeTimeout = 5 * time.Second
	httpExpectContinue      = 1 * time.Second
	httpMaxIdleConns        = 100
	httpMaxIdleConnsPerHost = 20
	httpIdleConnTimeout     = 90 * time.Second
	shutdownTimeout         = 10 * time.Second

	reportSubmitRPS    = 20
	reportSubmitBurst  = 30
	pdfRPS             = 5
	pdfBurst           = 8
	pdfRecipientsRPS   = 10
	pdfRecipientsBurst = 20
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	logger := newLogger(cfg.LogLevel)

	logger.Info("starting server",
		slog.String("port", cfg.Port),
		slog.Int("maxPairs", cfg.MaxPairs),
		slog.Int("requestBodyLimitMB", cfg.RequestBodyLimitMB),
		slog.Bool("requireHTTPS", cfg.RequireHTTPS),
		slog.Bool("uploadHTMLOnPDF", cfg.UploadHTMLOnPDF),
		slog.String("outputPrefix", cfg.OutputPrefix),
		slog.String("logLevel", cfg.LogLevel),
		slog.String("gotenbergURL", cfg.GotenbergURL),
		slog.String("b2Endpoint", cfg.B2Endpoint),
		slog.String("b2Bucket", cfg.B2Bucket),
		slog.String("publicBaseURL", cfg.PublicBaseURL),
		slog.Int("pdfWorkerCount", cfg.PDFWorkerCount),
		slog.Int("pdfQueueSize", cfg.PDFQueueSize),
		slog.Int("pdfSyncWaitSec", cfg.PDFSyncWaitSec),
		slog.Int("emailWorkerCount", cfg.EmailWorkerCount),
		slog.Int("emailQueueSize", cfg.EmailQueueSize),
	)

	sharedHTTPClient := &http.Client{
		Timeout: httpClientTimeout,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   httpDialTimeout,
				KeepAlive: httpKeepAlive,
			}).DialContext,
			MaxIdleConns:          httpMaxIdleConns,
			MaxIdleConnsPerHost:   httpMaxIdleConnsPerHost,
			IdleConnTimeout:       httpIdleConnTimeout,
			TLSHandshakeTimeout:   httpTLSHandshakeTimeout,
			ExpectContinueTimeout: httpExpectContinue,
		},
	}

	storageClient, err := storage.NewB2Storage(context.Background(), storage.Options{
		Endpoint:        cfg.B2Endpoint,
		Region:          cfg.B2Region,
		Bucket:          cfg.B2Bucket,
		AccessKeyID:     cfg.B2AccessKeyID,
		SecretAccessKey: cfg.B2SecretAccessKey,
		PublicBaseURL:   cfg.B2PublicBaseURL,
		HTTPClient:      sharedHTTPClient,
	})
	if err != nil {
		logger.Error("failed to initialize storage", slog.String("error", err.Error()))
		os.Exit(1)
	}

	pdfRenderer := gotenberg.NewClient(cfg.GotenbergURL, sharedHTTPClient)
	validate := validator.New()
	urlPolicy := security.NewURLPolicy(cfg.RequireHTTPS, cfg.ImageHostAllowlist)

	var store jobstore.Store
	var pdfStore jobstore.PDFStore
	if cfg.RedisURL != "" {
		redisOpts, err := redis.ParseURL(cfg.RedisURL)
		if err != nil {
			logger.Error("invalid REDIS_URL", slog.String("error", err.Error()))
			os.Exit(1)
		}
		redisClient := redis.NewClient(redisOpts)
		if err := redisClient.Ping(context.Background()).Err(); err != nil {
			logger.Error("redis unreachable", slog.String("error", err.Error()))
			os.Exit(1)
		}
		rs := jobstore.NewRedisStore(redisClient)
		store = rs
		pdfStore = rs
		logger.Info("using redis store", slog.String("url", cfg.RedisURL))
	} else {
		ms := jobstore.NewMemoryStore()
		store = ms
		pdfStore = ms
		logger.Info("using memory store")
	}

	notifier := pdfjobs.Notifier(pdfjobs.NewLogNotifier(logger))
	if cfg.EmailWebhookURL != "" {
		notifier = pdfjobs.NewWebhookNotifier(cfg.EmailWebhookURL, sharedHTTPClient)
		logger.Info("using webhook notifier", slog.String("emailWebhookURL", cfg.EmailWebhookURL))
	} else {
		logger.Info("using log notifier")
	}

	pdfService := pdfjobs.NewService(logger, pdfStore, storageClient, pdfRenderer, notifier, pdfjobs.Config{
		WorkerCount:      cfg.PDFWorkerCount,
		QueueSize:        cfg.PDFQueueSize,
		EmailWorkerCount: cfg.EmailWorkerCount,
		EmailQueueSize:   cfg.EmailQueueSize,
		SyncWaitTimeout:  time.Duration(cfg.PDFSyncWaitSec) * time.Second,
		OutputPrefix:     cfg.OutputPrefix,
		UploadHTMLOnPDF:  cfg.UploadHTMLOnPDF,
		LogoURL:          cfg.LogoURL,
	})
	if err := pdfService.Start(context.Background()); err != nil {
		logger.Error("failed to start pdf service", slog.String("error", err.Error()))
		os.Exit(1)
	}

	r := chi.NewRouter()
	r.Use(appmiddleware.RequestID)
	r.Use(appmiddleware.Recoverer(logger))
	r.Use(appmiddleware.SecurityHeaders)
	r.Use(appmiddleware.BodyLimit(cfg.BodyLimitBytes()))
	r.Use(appmiddleware.Logging(logger))

	r.Get("/health", handlers.NewHealthHandler().ServeHTTP)

	r.With(appmiddleware.RateLimit(rate.Limit(reportSubmitRPS), reportSubmitBurst, logger)).
		Post("/v1/reports",
			handlers.NewReportSubmitHandler(logger, validate, urlPolicy, store, storageClient, cfg.MaxPairs, cfg.OutputPrefix, cfg.LogoURL).ServeHTTP,
		)

	r.Get("/v1/reports/{id}",
		handlers.NewReportStatusHandler(logger, store).ServeHTTP,
	)

	r.With(appmiddleware.RateLimit(rate.Limit(pdfRPS), pdfBurst, logger)).
		Post("/v1/pdf",
			handlers.NewPDFHandler(logger, validate, urlPolicy, pdfService, cfg.MaxPairs).ServeHTTP,
		)

	r.With(appmiddleware.RateLimit(rate.Limit(pdfRecipientsRPS), pdfRecipientsBurst, logger)).
		Post("/v1/pdf/recipients",
			handlers.NewPDFRecipientsHandler(logger, validate, pdfService).ServeHTTP,
		)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("server ready", slog.String("port", cfg.Port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server stopped", slog.String("error", err.Error()))
			os.Exit(1)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	logger.Info("shutdown signal received", slog.String("signal", sig.String()))

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := pdfService.Stop(ctx); err != nil {
		logger.Error("pdf service stop failed", slog.String("error", err.Error()))
	}

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info("server shutdown complete")
}

func newLogger(level string) *slog.Logger {
	var l slog.Level
	switch level {
	case "debug":
		l = slog.LevelDebug
	case "warn":
		l = slog.LevelWarn
	case "error":
		l = slog.LevelError
	default:
		l = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: l}))
}
