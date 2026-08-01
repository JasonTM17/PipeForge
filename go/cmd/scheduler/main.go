package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/dataset"
	"github.com/JasonTM17/PipeForge/go/internal/multipart"
	"github.com/JasonTM17/PipeForge/go/internal/platform/config"
	"github.com/JasonTM17/PipeForge/go/internal/platform/database"
	"github.com/JasonTM17/PipeForge/go/internal/storage"
)

func main() {
	if err := run(context.Background()); err != nil {
		slog.Error("multipart cleanup failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	pool, err := database.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()
	objectStore, err := storage.NewMinIO(storage.MinIOConfig{
		Endpoint: cfg.MinIOEndpoint, PublicEndpoint: cfg.MinIOPublicEndpoint, AccessKey: cfg.MinIOAccessKey, SecretKey: cfg.MinIOSecretKey,
		Secure: cfg.MinIOSecure, Bucket: cfg.DatasetBucket,
	})
	if err != nil {
		return err
	}
	service, err := multipart.NewService(dataset.NewRepository(pool), multipart.NewRepository(pool), objectStore, multipart.Config{
		PartSize: cfg.MultipartPartSize, MaxParts: cfg.MultipartMaxParts, MaxBytes: cfg.MultipartMaxBytes,
		SessionTTL: cfg.MultipartSessionTTL, PartURLTTL: cfg.MultipartURLTTL,
	})
	if err != nil {
		return err
	}
	cleanupCtx, cancel := context.WithTimeout(ctx, maxCleanupDuration(cfg.ShutdownTimeout))
	defer cancel()
	report, err := service.CleanupExpired(cleanupCtx, cfg.MultipartCleanupLimit)
	if err != nil {
		return err
	}
	slog.Info("multipart cleanup completed", "claimed", report.Claimed, "expired", report.Expired, "failed", report.Failed)
	if len(report.Failures) > 0 {
		return fmt.Errorf("multipart cleanup completed with %d failures", len(report.Failures))
	}
	return nil
}

func maxCleanupDuration(shutdownTimeout time.Duration) time.Duration {
	if shutdownTimeout < 5*time.Minute {
		return 5 * time.Minute
	}
	return shutdownTimeout
}
