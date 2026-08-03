package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/JasonTM17/PipeForge/go/internal/dataset"
	"github.com/JasonTM17/PipeForge/go/internal/job"
	"github.com/JasonTM17/PipeForge/go/internal/lease"
	"github.com/JasonTM17/PipeForge/go/internal/multipart"
	"github.com/JasonTM17/PipeForge/go/internal/outbox"
	"github.com/JasonTM17/PipeForge/go/internal/platform/config"
	"github.com/JasonTM17/PipeForge/go/internal/platform/database"
	"github.com/JasonTM17/PipeForge/go/internal/queue"
	"github.com/JasonTM17/PipeForge/go/internal/scheduler"
	"github.com/JasonTM17/PipeForge/go/internal/storage"
	"github.com/JasonTM17/PipeForge/go/internal/workerregistry"
	"github.com/jackc/pgx/v5/pgxpool"
)

type schedulerRuntime struct {
	config           config.Config
	logger           *slog.Logger
	dispatcher       *scheduler.Dispatcher
	outboxDispatcher *outbox.Dispatcher
	leaseRepository  *lease.Repository
	workerRegistry   workerEventStore
	multipartService *multipart.Service
}

type workerEventStore interface {
	ApplyEnvelope(context.Context, queue.Envelope) error
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

	runtime, closeRuntime, err := newSchedulerRuntime(ctx, cfg, pool)
	if err != nil {
		return err
	}
	defer closeRuntime()
	return runtime.run(ctx)
}

func newSchedulerRuntime(ctx context.Context, cfg config.Config, pool *pgxpool.Pool) (*schedulerRuntime, func(), error) {
	if pool == nil {
		return nil, nil, fmt.Errorf("scheduler database pool is required")
	}
	outboxRepository, err := outbox.NewRepository(pool)
	if err != nil {
		return nil, nil, err
	}
	leaseConfig := lease.DefaultConfig()
	leaseConfig.LeaseDuration = cfg.LeaseDuration
	leaseConfig.RenewalWindow = cfg.LeaseRenewalWindow
	leaseConfig.SweepLimit = cfg.LeaseSweepLimit
	workerRepository, err := workerregistry.NewRepository(pool, workerregistry.Config{HeartbeatTTL: cfg.WorkerHeartbeatTTL})
	if err != nil {
		return nil, nil, err
	}
	leaseConfig.WorkerSelector = workerRepository
	leaseRepository, err := lease.NewRepository(pool, outboxRepository, leaseConfig)
	if err != nil {
		return nil, nil, err
	}
	jobRepository, err := job.NewRepository(pool, outboxRepository)
	if err != nil {
		return nil, nil, err
	}
	dispatcher, err := scheduler.NewDispatcher(
		scheduler.New(jobRepository, scheduler.Config{BatchSize: cfg.SchedulerBatchSize}),
		leaseRepository,
		scheduler.DispatchConfig{LeaseDuration: cfg.LeaseDuration},
	)
	if err != nil {
		return nil, nil, err
	}
	publisher, err := queue.NewPublisher(queue.PublisherConfig{URL: cfg.RabbitMQURL, ConfirmTimeout: cfg.ShutdownTimeout, DialTimeout: cfg.ShutdownTimeout})
	if err != nil {
		return nil, nil, err
	}
	if err := publisher.DeclareTopology(ctx, queue.DefaultTopology()); err != nil {
		_ = publisher.Close()
		return nil, nil, fmt.Errorf("declare scheduler topology: %w", err)
	}
	outboxDispatcher, err := outbox.NewDispatcher(outboxRepository, publisher, outbox.DispatcherConfig{})
	if err != nil {
		_ = publisher.Close()
		return nil, nil, err
	}
	objectStore, err := storage.NewMinIO(storage.MinIOConfig{
		Endpoint: cfg.MinIOEndpoint, PublicEndpoint: cfg.MinIOPublicEndpoint, AccessKey: cfg.MinIOAccessKey, SecretKey: cfg.MinIOSecretKey,
		Secure: cfg.MinIOSecure, PublicSecure: cfg.MinIOPublicSecure, Bucket: cfg.DatasetBucket,
	})
	if err != nil {
		_ = publisher.Close()
		return nil, nil, err
	}
	multipartService, err := multipart.NewService(dataset.NewRepository(pool), multipart.NewRepository(pool), objectStore, multipart.Config{
		PartSize: cfg.MultipartPartSize, MaxParts: cfg.MultipartMaxParts, MaxBytes: cfg.MultipartMaxBytes,
		SessionTTL: cfg.MultipartSessionTTL, PartURLTTL: cfg.MultipartURLTTL, CompletionGrace: cfg.MultipartCompletionGrace,
	})
	if err != nil {
		_ = publisher.Close()
		return nil, nil, err
	}
	return &schedulerRuntime{
		config: cfg, logger: slog.Default(), dispatcher: dispatcher, outboxDispatcher: outboxDispatcher,
		leaseRepository: leaseRepository, workerRegistry: workerRepository, multipartService: multipartService,
	}, func() { _ = publisher.Close() }, nil
}
