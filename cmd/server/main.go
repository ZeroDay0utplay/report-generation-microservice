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
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"golang.org/x/time/rate"

	"pdf-html-service/internal/config"
	"pdf-html-service/internal/gotenberg"
	"pdf-html-service/internal/handlers"
	"pdf-html-service/internal/jobstore"
	appmiddleware "pdf-html-service/internal/middleware"
	"pdf-html-service/internal/security"
	"pdf-html-service/internal/storage"
	"pdf-html-service/internal/worker"
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

	reportSubmitRPS   = 20
	reportSubmitBurst = 30
	pdfRPS            = 5
	pdfBurst          = 8

	asynqWorkerConcurrency = 10
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
		slog.String("publicBaseURL", cfg.PublicBaseURL),
		slog.Int("pdfChunkSize", cfg.PDFChunkSize),
		slog.Int("gotenbergConcurrency", cfg.GotenbergConcurrency),
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

	// Redis is required for async PDF generation.
	if cfg.RedisURL == "" {
		logger.Error("REDIS_URL is required for async PDF generation")
		os.Exit(1)
	}

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

	store := jobstore.NewRedisStore(redisClient)
	logger.Info("using redis store", slog.String("url", cfg.RedisURL))

	// Asynq uses its own Redis connection (specified via RedisClientOpt).
	asynqRedisOpt := asynq.RedisClientOpt{
		Addr:     redisOpts.Addr,
		Password: redisOpts.Password,
		DB:       redisOpts.DB,
	}

	asynqClient := asynq.NewClient(asynqRedisOpt)
	defer asynqClient.Close()

	// Build the processor.
	processor := worker.NewProcessor(worker.ProcessorConfig{
		Store:     store,
		Gotenberg: pdfRenderer,
		Storage:   storageClient,
		HTTPClient: sharedHTTPClient,
		LogoURL:      cfg.LogoURL,
		OutputPrefix: cfg.OutputPrefix,
		ChunkSize:    cfg.PDFChunkSize,
		Concurrency:  cfg.GotenbergConcurrency,
		ChunkTimeout: time.Duration(cfg.ChunkTimeoutSec) * time.Second,
		MergeTimeout: time.Duration(cfg.MergeTimeoutSec) * time.Second,
		DownloadTTL:  time.Duration(cfg.DownloadURLTTLHours) * time.Hour,
		SMTP: worker.SMTPConfig{
			Host: cfg.SMTPHost,
			Port: cfg.SMTPPort,
			User: cfg.SMTPUser,
			Pass: cfg.SMTPPass,
			From: cfg.SMTPFrom,
		},
		Logger: logger,
	})

	// Start Asynq worker server.
	asynqSrv := asynq.NewServer(asynqRedisOpt, asynq.Config{
		Concurrency: asynqWorkerConcurrency,
		Logger:      newAsynqLogger(logger),
	})

	mux := asynq.NewServeMux()
	mux.HandleFunc(worker.TypePDFGenerate, processor.ProcessTask)

	go func() {
		if err := asynqSrv.Run(mux); err != nil {
			logger.Error("asynq server stopped", slog.String("error", err.Error()))
			os.Exit(1)
		}
	}()

	r := chi.NewRouter()
	r.Use(appmiddleware.RequestID)
	r.Use(appmiddleware.Recoverer(logger))
	r.Use(appmiddleware.SecurityHeaders)
	r.Use(appmiddleware.BodyLimit(cfg.BodyLimitBytes()))
	r.Use(appmiddleware.Logging(logger))

	r.Get("/health", handlers.NewHealthHandler().ServeHTTP)

	r.With(appmiddleware.RateLimit(rate.Limit(reportSubmitRPS), reportSubmitBurst, logger)).
		Post("/v1/reports",
			handlers.NewReportSubmitHandler(logger, validate, urlPolicy, store, cfg.MaxPairs, cfg.PublicBaseURL).ServeHTTP,
		)

	r.Get("/v1/reports/{id}",
		handlers.NewReportStatusHandler(logger, store).ServeHTTP,
	)

	r.Get("/v1/reports/{id}/html",
		handlers.NewReportHTMLHandler(logger, store, cfg.LogoURL).ServeHTTP,
	)

	r.With(appmiddleware.RateLimit(rate.Limit(pdfRPS), pdfBurst, logger)).
		Post("/v1/pdf",
			handlers.NewPDFHandler(logger, validate, urlPolicy, store, asynqClient, cfg.MaxPairs, cfg.OutputPrefix, cfg.LogoURL).ServeHTTP,
		)

	r.Get("/v1/pdf/status/{jobId}",
		handlers.NewPDFStatusHandler(logger, store).ServeHTTP,
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
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	asynqSrv.Shutdown()
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

// asynqLogger bridges slog to the asynq.Logger interface.
type asynqLogger struct{ l *slog.Logger }

func newAsynqLogger(l *slog.Logger) *asynqLogger { return &asynqLogger{l: l} }

func (a *asynqLogger) Debug(args ...interface{}) { a.l.Debug(fmt.Sprint(args...)) }
func (a *asynqLogger) Info(args ...interface{})  { a.l.Info(fmt.Sprint(args...)) }
func (a *asynqLogger) Warn(args ...interface{})  { a.l.Warn(fmt.Sprint(args...)) }
func (a *asynqLogger) Error(args ...interface{}) { a.l.Error(fmt.Sprint(args...)) }
func (a *asynqLogger) Fatal(args ...interface{}) {
	a.l.Error(fmt.Sprint(args...))
	os.Exit(1)
}
