package queue

import (
	"fmt"

	"github.com/rabbitmq/amqp091-go"
)

type Exchange struct {
	Name string
	Kind string
}

type Queue struct {
	Name       string
	DeadLetter string
}

type Binding struct {
	Exchange string
	Queue    string
	Key      string
}

type Topology struct {
	Exchanges []Exchange
	Queues    []Queue
	Bindings  []Binding
}

func DefaultTopology() Topology {
	return Topology{
		Exchanges: []Exchange{{CommandsExchange, "topic"}, {EventsExchange, "topic"}, {DeadLetterExchange, "topic"}},
		Queues: []Queue{
			{Name: "processing.jobs", DeadLetter: "processing.jobs.dlq"},
			{Name: "processing.cancellations", DeadLetter: "processing.cancellations.dlq"},
			{Name: "control-plane.results", DeadLetter: "control-plane.results.dlq"},
			{Name: "audit.events"}, {Name: "monitoring.events"},
			{Name: "processing.jobs.dlq"}, {Name: "processing.cancellations.dlq"}, {Name: "control-plane.results.dlq"},
		},
		Bindings: []Binding{
			{CommandsExchange, "processing.jobs", "processing.job.requested"},
			{CommandsExchange, "processing.cancellations", "processing.job.cancel-requested"},
			{EventsExchange, "control-plane.results", "processing.job.#"},
			{EventsExchange, "control-plane.results", "processing.artifact.created"},
			{EventsExchange, "audit.events", "audit.#"},
			{EventsExchange, "monitoring.events", "monitoring.#"},
			{DeadLetterExchange, "processing.jobs.dlq", "processing.jobs.dlq"},
			{DeadLetterExchange, "processing.cancellations.dlq", "processing.cancellations.dlq"},
			{DeadLetterExchange, "control-plane.results.dlq", "control-plane.results.dlq"},
		},
	}
}

func declareTopology(channel *amqp091.Channel, topology Topology) error {
	for _, exchange := range topology.Exchanges {
		if err := channel.ExchangeDeclare(exchange.Name, exchange.Kind, true, false, false, false, nil); err != nil {
			return fmt.Errorf("declare exchange %s: %w", exchange.Name, err)
		}
	}
	for _, queue := range topology.Queues {
		arguments := amqp091.Table{}
		if queue.DeadLetter != "" {
			arguments["x-dead-letter-exchange"] = DeadLetterExchange
			arguments["x-dead-letter-routing-key"] = queue.DeadLetter
		}
		if _, err := channel.QueueDeclare(queue.Name, true, false, false, false, arguments); err != nil {
			return fmt.Errorf("declare queue %s: %w", queue.Name, err)
		}
	}
	for _, binding := range topology.Bindings {
		if err := channel.QueueBind(binding.Queue, binding.Key, binding.Exchange, false, nil); err != nil {
			return fmt.Errorf("bind queue %s: %w", binding.Queue, err)
		}
	}
	return nil
}
