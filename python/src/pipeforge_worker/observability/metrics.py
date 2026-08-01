"""Prometheus metrics owned by one injected worker instance."""

from __future__ import annotations

from prometheus_client import CollectorRegistry, Counter, Gauge, Histogram, generate_latest


class WorkerMetrics:
    def __init__(self, registry: CollectorRegistry | None = None) -> None:
        self.registry = registry or CollectorRegistry()
        self.messages_received = Counter(
            "pipeforge_worker_messages_received_total",
            "Messages received by the worker consumer.",
            ["message_type"],
            registry=self.registry,
        )
        self.messages_acked = Counter(
            "pipeforge_worker_messages_acked_total",
            "Messages acknowledged after durable handling.",
            ["message_type"],
            registry=self.registry,
        )
        self.messages_rejected = Counter(
            "pipeforge_worker_messages_rejected_total",
            "Messages rejected to the configured dead-letter path.",
            ["reason"],
            registry=self.registry,
        )
        self.heartbeat_publications = Counter(
            "pipeforge_worker_heartbeat_publications_total",
            "Worker registration and heartbeat publications.",
            ["message_type"],
            registry=self.registry,
        )
        self.active_jobs = Gauge(
            "pipeforge_worker_active_jobs",
            "Bounded number of currently active job identifiers.",
            registry=self.registry,
        )
        self.status = Gauge(
            "pipeforge_worker_status",
            "Worker status as a one-hot gauge.",
            ["status"],
            registry=self.registry,
        )
        self.handler_duration_seconds = Histogram(
            "pipeforge_worker_handler_duration_seconds",
            "Job command handler duration.",
            registry=self.registry,
        )

    def set_status(self, status: str) -> None:
        for known_status in ("STARTING", "READY", "BUSY", "DRAINING", "UNHEALTHY", "OFFLINE"):
            self.status.labels(status=known_status).set(1 if known_status == status else 0)

    def render(self) -> bytes:
        return generate_latest(self.registry)
