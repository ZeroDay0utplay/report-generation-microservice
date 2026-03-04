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
	"golang.org/x/time/rate"

	"pdf-html-service/internal/config"
	"pdf-html-service/internal/gotenberg"
	"pdf-html-service/internal/handlers"
	"pdf-html-service/internal/jobstore"
	appmiddleware "pdf-html-service/internal/middleware"
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

	reportSubmitRPS   = 20
	reportSubmitBurst = 30
	pdfRPS            = 5
	pdfBurst          = 8
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config error: %v\n", err)
		os.Exit(1)
	}

	logger := newLogger(cfg.LogLevel)

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
	store := jobstore.NewMemoryStore()

	r := chi.NewRouter()
	r.Use(appmiddleware.RequestID)
	r.Use(appmiddleware.Recoverer(logger))
	r.Use(appmiddleware.SecurityHeaders)
	r.Use(appmiddleware.BodyLimit(cfg.BodyLimitBytes()))
	r.Use(appmiddleware.Logging(logger))

	r.Get("/health", handlers.NewHealthHandler().ServeHTTP)

	r.With(appmiddleware.RateLimit(rate.Limit(reportSubmitRPS), reportSubmitBurst)).
		Post("/v1/reports",
			handlers.NewReportSubmitHandler(logger, validate, urlPolicy, store, cfg.MaxPairs, cfg.PublicBaseURL).ServeHTTP,
		)

	r.Get("/v1/reports/{id}",
		handlers.NewReportStatusHandler(logger, store).ServeHTTP,
	)

	r.Get("/v1/reports/{id}/html",
		handlers.NewReportHTMLHandler(logger, store, cfg.LogoURL).ServeHTTP,
	)

	r.With(appmiddleware.RateLimit(rate.Limit(pdfRPS), pdfBurst)).
		Post("/v1/pdf",
			handlers.NewPDFHandler(logger, validate, urlPolicy, storageClient, pdfRenderer, cfg.MaxPairs, cfg.OutputPrefix, cfg.UploadHTMLOnPDF).ServeHTTP,
		)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("server listening",
			slog.String("port", cfg.Port),
			slog.String("gotenbergUrl", cfg.GotenbergURL),
		)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server stopped", slog.String("error", err.Error()))
			os.Exit(1)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
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
