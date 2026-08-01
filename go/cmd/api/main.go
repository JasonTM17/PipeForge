package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/JasonTM17/PipeForge/go/internal/httpapi"
	"github.com/JasonTM17/PipeForge/go/internal/observability"
	"github.com/JasonTM17/PipeForge/go/internal/platform/config"
	"github.com/JasonTM17/PipeForge/go/internal/platform/database"
)

func main() {
	if err := run(); err != nil {
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger.Info("starting pipeforge api", "environment", cfg.Environment, "address", cfg.HTTPAddr)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := database.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	metrics := observability.NewMetrics()
	router := httpapi.NewRouter(httpapi.Dependencies{
		Logger:  logger,
		Metrics: metrics,
		Readiness: func(ctx context.Context) error {
			return pool.Ping(ctx)
		},
	})
	server := &http.Server{Addr: cfg.HTTPAddr, Handler: router}

	serverErr := make(chan error, 1)
	go func() {
		if listenErr := server.ListenAndServe(); !errors.Is(listenErr, http.ErrServerClosed) {
			serverErr <- listenErr
		}
		close(serverErr)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	case serverErr := <-serverErr:
		return serverErr
	}
}
