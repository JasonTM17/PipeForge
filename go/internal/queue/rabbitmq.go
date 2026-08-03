package queue

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/rabbitmq/amqp091-go"
)

const (
	CommandsExchange      = "pipeforge.commands"
	EventsExchange        = "pipeforge.events"
	DeadLetterExchange    = "pipeforge.dead-letter"
	DefaultConfirmTimeout = 10 * time.Second
)

var (
	ErrPublisherNack   = errors.New("RabbitMQ publisher confirm was negative")
	ErrPublisherClosed = errors.New("RabbitMQ publisher is closed")
)

type PublisherConfig struct {
	URL            string
	ConfirmTimeout time.Duration
	DialTimeout    time.Duration
	MaxMessageSize int
}

func (c PublisherConfig) validate() error {
	if c.URL == "" {
		return errors.New("RabbitMQ URL is required")
	}
	if c.ConfirmTimeout <= 0 {
		return errors.New("RabbitMQ confirm timeout must be positive")
	}
	if c.DialTimeout <= 0 {
		return errors.New("RabbitMQ dial timeout must be positive")
	}
	if c.MaxMessageSize < 1 || c.MaxMessageSize > MaxEnvelopeBytes {
		return fmt.Errorf("RabbitMQ max message size must be between 1 and %d", MaxEnvelopeBytes)
	}
	return nil
}

type Publisher struct {
	mu            sync.Mutex
	config        PublisherConfig
	connection    *amqp091.Connection
	channel       *amqp091.Channel
	confirmations <-chan amqp091.Confirmation
	closed        bool
}

func NewPublisher(config PublisherConfig) (*Publisher, error) {
	if config.ConfirmTimeout == 0 {
		config.ConfirmTimeout = DefaultConfirmTimeout
	}
	if config.DialTimeout == 0 {
		config.DialTimeout = config.ConfirmTimeout
	}
	if config.MaxMessageSize == 0 {
		config.MaxMessageSize = MaxEnvelopeBytes
	}
	if err := config.validate(); err != nil {
		return nil, err
	}
	publisher := &Publisher{config: config}
	publisher.mu.Lock()
	defer publisher.mu.Unlock()
	if err := publisher.connectLocked(); err != nil {
		return nil, err
	}
	return publisher, nil
}

func (p *Publisher) connectLocked() error {
	if p.closed {
		return ErrPublisherClosed
	}
	connection, err := Dial(p.config.URL, p.config.DialTimeout)
	if err != nil {
		return fmt.Errorf("dial RabbitMQ: %w", err)
	}
	channel, err := connection.Channel()
	if err != nil {
		_ = connection.Close()
		return fmt.Errorf("open RabbitMQ channel: %w", err)
	}
	if err := channel.Confirm(false); err != nil {
		_ = channel.Close()
		_ = connection.Close()
		return fmt.Errorf("enable RabbitMQ publisher confirms: %w", err)
	}
	p.connection = connection
	p.channel = channel
	p.confirmations = channel.NotifyPublish(make(chan amqp091.Confirmation, 1))
	return nil
}

// Dial establishes a RabbitMQ connection with a bounded TCP and AMQP handshake.
// The returned connection has its temporary handshake deadline cleared.
func Dial(url string, timeout time.Duration) (*amqp091.Connection, error) {
	if timeout <= 0 {
		return nil, errors.New("RabbitMQ dial timeout must be positive")
	}
	dialer := net.Dialer{Timeout: timeout}
	var transport net.Conn
	connection, err := amqp091.DialConfig(url, amqp091.Config{
		Dial: func(network, address string) (net.Conn, error) {
			conn, dialErr := dialer.Dial(network, address)
			if dialErr != nil {
				return nil, dialErr
			}
			if deadlineErr := conn.SetDeadline(time.Now().Add(timeout)); deadlineErr != nil {
				_ = conn.Close()
				return nil, deadlineErr
			}
			transport = conn
			return conn, nil
		},
	})
	if transport != nil {
		_ = transport.SetDeadline(time.Time{})
	}
	if err != nil {
		return nil, fmt.Errorf("dial RabbitMQ: %w", err)
	}
	return connection, nil
}

func (p *Publisher) resetLocked() {
	if p.channel != nil {
		_ = p.channel.Close()
	}
	if p.connection != nil {
		_ = p.connection.Close()
	}
	p.channel = nil
	p.connection = nil
	p.confirmations = nil
}

func (p *Publisher) Publish(ctx context.Context, exchange, routingKey string, envelope Envelope) error {
	if p == nil {
		return ErrPublisherClosed
	}
	body, err := MarshalEnvelope(envelope)
	if err != nil {
		return fmt.Errorf("validate message before publish: %w", err)
	}
	if len(body) > p.config.MaxMessageSize {
		return fmt.Errorf("message exceeds configured limit of %d bytes", p.config.MaxMessageSize)
	}
	if exchange == "" || routingKey == "" {
		return errors.New("RabbitMQ exchange and routing key are required")
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrPublisherClosed
	}
	for attempt := 0; attempt < 2; attempt++ {
		if p.channel == nil || p.connection == nil || p.connection.IsClosed() {
			p.resetLocked()
			if err := p.connectLocked(); err != nil {
				return err
			}
		}
		publishErr := p.channel.PublishWithContext(ctx, exchange, routingKey, false, false, amqp091.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp091.Persistent,
			MessageId:    envelope.MessageID.String(),
			Type:         envelope.MessageType,
			Timestamp:    envelope.OccurredAt,
			Headers: amqp091.Table{
				"x-schema-version": envelope.SchemaVersion,
				"x-trace-id":       envelope.TraceID,
				"x-correlation-id": envelope.CorrelationID,
				"x-causation-id":   envelope.CausationID,
			},
			Body: body,
		})
		if publishErr != nil {
			p.resetLocked()
			if attempt == 0 {
				continue
			}
			return fmt.Errorf("publish RabbitMQ message: %w", publishErr)
		}
		confirmed, ok := waitForConfirmation(ctx, p.confirmations, p.config.ConfirmTimeout)
		if ok && confirmed.Ack {
			return nil
		}
		p.resetLocked()
		if ok && !confirmed.Ack {
			return ErrPublisherNack
		}
		if attempt == 1 {
			return errors.New("RabbitMQ publisher confirmation timed out or connection closed")
		}
	}
	return errors.New("RabbitMQ publish failed")
}

func (p *Publisher) DeclareTopology(ctx context.Context, topology Topology) error {
	if p == nil {
		return ErrPublisherClosed
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.closed {
		return ErrPublisherClosed
	}
	if p.channel == nil || p.connection == nil || p.connection.IsClosed() {
		p.resetLocked()
		if err := p.connectLocked(); err != nil {
			return err
		}
	}
	if err := declareTopology(p.channel, topology); err != nil {
		p.resetLocked()
		return err
	}
	return nil
}

func (p *Publisher) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	if p.channel != nil {
		_ = p.channel.Close()
	}
	if p.connection != nil {
		return p.connection.Close()
	}
	return nil
}
