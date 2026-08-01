package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/JasonTM17/PipeForge/go/internal/platform/config"
	"github.com/JasonTM17/PipeForge/go/internal/platform/database"
	"github.com/JasonTM17/PipeForge/go/internal/queue"
	"github.com/JasonTM17/PipeForge/go/internal/result"
	"github.com/rabbitmq/amqp091-go"
)

const resultQueue = "control-plane.results"

func main() {
	if err := run(); err != nil {
		slog.Error("result consumer stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := database.Open(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	resultRepository, err := result.NewRepository(pool)
	if err != nil {
		return err
	}
	resultService, err := result.NewService(resultRepository, nil)
	if err != nil {
		return err
	}

	declarationPublisher, err := queue.NewPublisher(queue.PublisherConfig{URL: cfg.RabbitMQURL})
	if err != nil {
		return err
	}
	defer declarationPublisher.Close()
	if err := declarationPublisher.DeclareTopology(ctx, queue.DefaultTopology()); err != nil {
		return fmt.Errorf("declare result consumer topology: %w", err)
	}

	connection, err := amqp091.Dial(cfg.RabbitMQURL)
	if err != nil {
		return fmt.Errorf("dial result consumer RabbitMQ: %w", err)
	}
	defer connection.Close()
	channel, err := connection.Channel()
	if err != nil {
		return fmt.Errorf("open result consumer channel: %w", err)
	}
	defer channel.Close()
	if err := channel.Qos(16, 0, false); err != nil {
		return fmt.Errorf("configure result consumer QoS: %w", err)
	}
	deliveries, err := channel.Consume(resultQueue, "", false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consume result queue: %w", err)
	}

	logger.Info("started pipeforge result consumer", "queue", resultQueue)
	for {
		select {
		case <-ctx.Done():
			return nil
		case delivery, ok := <-deliveries:
			if !ok {
				return errors.New("result queue delivery channel closed")
			}
			if err := result.HandleDelivery(ctx, resultService, delivery, delivery.Body); err != nil {
				logger.ErrorContext(ctx, "result delivery handling failed", "message_id", delivery.MessageId, "message_type", delivery.Type, "error", err)
				continue
			}
			logger.DebugContext(ctx, "result delivery processed", "message_id", delivery.MessageId, "message_type", delivery.Type)
		}
	}
}
