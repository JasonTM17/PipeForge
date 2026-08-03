package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/JasonTM17/PipeForge/go/internal/queue"
)

const (
	dispatchQueue             = "control-plane.dispatch"
	workerEventsQueue         = "control-plane.workers"
	queuedJobMessageType      = queue.MessageJobQueued
	schedulerConsumerPrefetch = 8
)

func (r *schedulerRuntime) run(ctx context.Context) error {
	if r == nil || r.dispatcher == nil || r.outboxDispatcher == nil || r.leaseRepository == nil || r.workerRegistry == nil || r.multipartService == nil {
		return errors.New("scheduler runtime is not configured")
	}
	if err := r.runStartup(ctx); err != nil {
		return err
	}

	connection, err := queue.Dial(r.config.RabbitMQURL, r.config.ShutdownTimeout)
	if err != nil {
		return fmt.Errorf("dial scheduler RabbitMQ: %w", err)
	}
	defer connection.Close()
	channel, err := connection.Channel()
	if err != nil {
		return fmt.Errorf("open scheduler RabbitMQ channel: %w", err)
	}
	defer channel.Close()
	if err := channel.Qos(schedulerConsumerPrefetch, 0, false); err != nil {
		return fmt.Errorf("configure scheduler consumer QoS: %w", err)
	}
	deliveries, err := channel.Consume(dispatchQueue, "", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consume scheduler queue: %w", err)
	}
	workerDeliveries, err := channel.Consume(workerEventsQueue, "", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consume worker events queue: %w", err)
	}

	dispatchTicker := time.NewTicker(r.config.SchedulerDispatchInterval)
	defer dispatchTicker.Stop()
	outboxTicker := time.NewTicker(r.config.OutboxDispatchInterval)
	defer outboxTicker.Stop()
	maintenanceTicker := time.NewTicker(r.config.MaintenanceInterval)
	defer maintenanceTicker.Stop()
	r.logger.Info("started PipeForge scheduler", "dispatch_queue", dispatchQueue, "worker_events_queue", workerEventsQueue, "worker_heartbeat_ttl", r.config.WorkerHeartbeatTTL)

	for {
		select {
		case <-ctx.Done():
			return nil
		case delivery, ok := <-deliveries:
			if !ok {
				return errors.New("scheduler delivery channel closed")
			}
			r.handleDelivery(ctx, delivery)
		case delivery, ok := <-workerDeliveries:
			if !ok {
				return errors.New("worker events delivery channel closed")
			}
			r.handleWorkerDelivery(ctx, delivery)
		case <-dispatchTicker.C:
			r.logCycleError(ctx, "scheduler reconciliation", r.dispatchEligible)
		case <-outboxTicker.C:
			r.logCycleError(ctx, "outbox dispatch", r.dispatchOutbox)
		case <-maintenanceTicker.C:
			r.logCycleError(ctx, "scheduler maintenance", r.runMaintenance)
		}
	}
}

func (r *schedulerRuntime) runStartup(ctx context.Context) error {
	r.logCycleError(ctx, "initial outbox dispatch", r.dispatchOutbox)
	r.logCycleError(ctx, "initial scheduler reconciliation", r.dispatchEligible)
	r.logCycleError(ctx, "initial leased-command dispatch", r.dispatchOutbox)
	r.logCycleError(ctx, "initial scheduler maintenance", r.runMaintenance)
	return nil
}

func (r *schedulerRuntime) dispatchEligible(ctx context.Context) error {
	report, err := r.dispatcher.Dispatch(ctx)
	if err != nil {
		return err
	}
	if len(report.Failures) > 0 {
		return fmt.Errorf("lease acquisition had %d failures: %w", len(report.Failures), errors.Join(report.Failures...))
	}
	if report.Selected > 0 {
		r.logger.InfoContext(ctx, "scheduler reconciliation completed", "selected", report.Selected, "acquired", report.Acquired, "skipped", report.Skipped)
	}
	return nil
}

func (r *schedulerRuntime) dispatchOutbox(ctx context.Context) error {
	report, err := r.outboxDispatcher.Dispatch(ctx, r.config.OutboxBatchSize)
	if err != nil {
		return err
	}
	if len(report.Failures) > 0 {
		return fmt.Errorf("outbox dispatch had %d failures: %w", len(report.Failures), errors.Join(report.Failures...))
	}
	if report.Claimed > 0 {
		r.logger.InfoContext(ctx, "outbox dispatch completed", "claimed", report.Claimed, "published", report.Published, "retried", report.Retried, "poisoned", report.Poisoned)
	}
	return nil
}

func (r *schedulerRuntime) runMaintenance(ctx context.Context) error {
	cleanupCtx, cancel := context.WithTimeout(ctx, r.config.ShutdownTimeout)
	defer cancel()
	cleanupReport, err := r.multipartService.CleanupExpired(cleanupCtx, r.config.MultipartCleanupLimit)
	if err != nil {
		return err
	}
	if len(cleanupReport.Failures) > 0 {
		return fmt.Errorf("multipart cleanup had %d failures", len(cleanupReport.Failures))
	}
	leaseReport, err := r.leaseRepository.SweepExpired(cleanupCtx, r.config.LeaseSweepLimit)
	if err != nil {
		return err
	}
	r.logger.InfoContext(ctx, "scheduler maintenance completed", "multipart_claimed", cleanupReport.Claimed, "lease_claimed", leaseReport.Claimed, "lease_retried", leaseReport.Retried, "lease_dead_lettered", leaseReport.DeadLettered)
	return nil
}

func (r *schedulerRuntime) logCycleError(ctx context.Context, name string, run func(context.Context) error) {
	if err := run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		r.logger.ErrorContext(ctx, name+" failed", "error", err)
	}
}
