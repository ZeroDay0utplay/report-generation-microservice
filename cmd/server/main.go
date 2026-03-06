package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
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
	"pdf-html-service/internal/notify"
	"pdf-html-service/internal/pipeline"
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

	reportRPS   = 20
	reportBurst = 30
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
		slog.String("outputPrefix", cfg.OutputPrefix),
		slog.String("logLevel", cfg.LogLevel),
		slog.String("gotenbergURL", cfg.GotenbergURL),
		slog.String("b2Endpoint", cfg.B2Endpoint),
		slog.String("b2Bucket", cfg.B2Bucket),
		slog.Int("syncTimeoutSec", cfg.SyncTimeoutSec),
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
	mailer := notify.NewSMTPSender(cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPUser, cfg.SMTPPass, cfg.SMTPFrom)

	var store jobstore.Store
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
		store = jobstore.NewRedisStore(redisClient)
		logger.Info("using redis store")
	} else {
		store = jobstore.NewMemoryStore()
		logger.Info("using memory store")
	}

	pipe := pipeline.New(store, storageClient, pdfRenderer, mailer, logger, cfg.OutputPrefix, cfg.LogoURL)
	var bgWg sync.WaitGroup
	syncTimeout := time.Duration(cfg.SyncTimeoutSec) * time.Second

	r := chi.NewRouter()
	r.Use(appmiddleware.RequestID)
	r.Use(appmiddleware.Recoverer(logger))
	r.Use(appmiddleware.SecurityHeaders)
	r.Use(appmiddleware.BodyLimit(cfg.BodyLimitBytes()))
	r.Use(appmiddleware.Logging(logger))

	r.Get("/health", handlers.NewHealthHandler().ServeHTTP)

	r.Route("/reports", func(r chi.Router) {
		r.Use(appmiddleware.RateLimit(rate.Limit(reportRPS), reportBurst, logger))

		r.Post("/", handlers.NewReportHandler(
			logger, validate, urlPolicy, store, pipe, &bgWg, cfg.MaxPairs, syncTimeout,
		).ServeHTTP)

		r.Get("/{jobID}", handlers.NewReportStatusHandler(logger, store).ServeHTTP)

		r.Post("/{jobID}/notify", handlers.NewReportNotifyHandler(
			logger, validate, store, mailer,
		).ServeHTTP)
	})

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
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	logger.Info("waiting for background jobs to finish")
	bgWg.Wait()
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
