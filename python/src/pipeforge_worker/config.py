"""Validated worker settings with explicit environment aliases."""

from __future__ import annotations

import socket
from pathlib import Path
from typing import Any
from uuid import UUID, uuid4

from pydantic import AliasChoices, BaseModel, Field, field_validator, model_validator
from pydantic_settings import BaseSettings, SettingsConfigDict

from pipeforge_worker import __version__


def default_contracts_dir() -> Path:
    configured = Path(__import__("os").environ.get("PIPEFORGE_CONTRACTS_DIR", ""))
    if str(configured) not in {"", "."} and configured.exists():
        return configured
    current = Path(__file__).resolve()
    for parent in current.parents:
        candidate = parent / "contracts" / "json-schema"
        if candidate.is_dir():
            return candidate
    return Path("/opt/pipeforge/contracts/json-schema")


class Settings(BaseSettings):
    """Runtime configuration; all values are bounded before clients start."""

    model_config = SettingsConfigDict(
        env_prefix="",
        case_sensitive=False,
        extra="ignore",
        validate_default=True,
        populate_by_name=True,
    )

    environment: str = Field(
        default="development",
        validation_alias=AliasChoices("PIPEFORGE_WORKER_ENV", "PIPEFORGE_ENV"),
    )
    log_level: str = Field(
        default="INFO",
        validation_alias=AliasChoices("PIPEFORGE_WORKER_LOG_LEVEL", "PIPEFORGE_LOG_LEVEL"),
    )
    worker_id: UUID = Field(
        default_factory=uuid4, validation_alias=AliasChoices("PIPEFORGE_WORKER_ID", "WORKER_ID")
    )
    instance_id: str = Field(
        default_factory=lambda: f"{socket.gethostname()}-{uuid4().hex[:12]}",
        validation_alias=AliasChoices("PIPEFORGE_WORKER_INSTANCE_ID", "WORKER_INSTANCE_ID"),
    )
    hostname: str = Field(
        default_factory=socket.gethostname,
        validation_alias=AliasChoices("PIPEFORGE_WORKER_HOSTNAME", "WORKER_HOSTNAME"),
    )
    software_version: str = Field(
        default=__version__,
        validation_alias=AliasChoices("PIPEFORGE_WORKER_VERSION", "WORKER_VERSION"),
    )
    supported_operations: tuple[str, ...] = Field(
        default=(
            "PROFILE_DATASET",
            "CHECK_MISSING_VALUES",
            "CHECK_DUPLICATES",
            "VALIDATE_QUALITY",
            "DETECT_OUTLIERS",
        ),
        validation_alias=AliasChoices("PIPEFORGE_WORKER_OPERATIONS", "WORKER_OPERATIONS"),
    )
    max_concurrency: int = Field(
        default=2,
        validation_alias=AliasChoices("PIPEFORGE_WORKER_MAX_CONCURRENCY", "WORKER_MAX_CONCURRENCY"),
    )
    prefetch_count: int = Field(
        default=2, validation_alias=AliasChoices("PIPEFORGE_WORKER_PREFETCH", "WORKER_PREFETCH")
    )

    broker_url: str = Field(
        default="amqp://pipeforge:pipeforge-local-rabbitmq@localhost:55672/",
        validation_alias=AliasChoices("PIPEFORGE_WORKER_BROKER_URL", "RABBITMQ_URL"),
    )
    broker_queue: str = Field(
        default="processing.jobs",
        validation_alias=AliasChoices("PIPEFORGE_WORKER_QUEUE", "WORKER_QUEUE"),
    )
    cancellation_queue: str = Field(
        default="processing.cancellations",
        validation_alias=AliasChoices(
            "PIPEFORGE_WORKER_CANCELLATION_QUEUE", "WORKER_CANCELLATION_QUEUE"
        ),
    )
    dead_letter_exchange: str = Field(
        default="pipeforge.dead-letter",
        validation_alias=AliasChoices("PIPEFORGE_WORKER_DLX", "WORKER_DLX"),
    )
    dead_letter_routing_key: str = Field(
        default="processing.jobs.dlq",
        validation_alias=AliasChoices("PIPEFORGE_WORKER_DLX_ROUTING_KEY", "WORKER_DLX_ROUTING_KEY"),
    )
    cancellation_dead_letter_routing_key: str = Field(
        default="processing.cancellations.dlq",
        validation_alias=AliasChoices(
            "PIPEFORGE_WORKER_CANCELLATION_DLX_ROUTING_KEY",
            "WORKER_CANCELLATION_DLX_ROUTING_KEY",
        ),
    )
    commands_exchange: str = Field(
        default="pipeforge.commands",
        validation_alias=AliasChoices(
            "PIPEFORGE_WORKER_COMMANDS_EXCHANGE", "WORKER_COMMANDS_EXCHANGE"
        ),
    )
    events_exchange: str = Field(
        default="pipeforge.events",
        validation_alias=AliasChoices("PIPEFORGE_WORKER_EVENTS_EXCHANGE", "WORKER_EVENTS_EXCHANGE"),
    )
    max_message_bytes: int = Field(
        default=1 << 20,
        validation_alias=AliasChoices(
            "PIPEFORGE_WORKER_MAX_MESSAGE_BYTES", "WORKER_MAX_MESSAGE_BYTES"
        ),
    )
    reconnect_delay_seconds: float = Field(
        default=2.0,
        validation_alias=AliasChoices("PIPEFORGE_WORKER_RECONNECT_DELAY", "WORKER_RECONNECT_DELAY"),
    )

    minio_endpoint: str = Field(
        default="localhost:59010",
        validation_alias=AliasChoices("PIPEFORGE_WORKER_MINIO_ENDPOINT", "MINIO_ENDPOINT"),
    )
    minio_access_key: str = Field(
        default="pipeforge",
        validation_alias=AliasChoices("PIPEFORGE_WORKER_MINIO_ACCESS_KEY", "MINIO_ACCESS_KEY"),
    )
    minio_secret_key: str = Field(
        default="pipeforge-local-minio-secret",
        validation_alias=AliasChoices("PIPEFORGE_WORKER_MINIO_SECRET_KEY", "MINIO_SECRET_KEY"),
    )
    minio_secure: bool = Field(
        default=False,
        validation_alias=AliasChoices("PIPEFORGE_WORKER_MINIO_SECURE", "MINIO_SECURE"),
    )
    dataset_bucket: str = Field(
        default="datasets",
        validation_alias=AliasChoices("PIPEFORGE_WORKER_DATASET_BUCKET", "MINIO_DATASET_BUCKET"),
    )
    artifact_bucket: str = Field(
        default="artifacts",
        validation_alias=AliasChoices("PIPEFORGE_WORKER_ARTIFACT_BUCKET", "MINIO_ARTIFACT_BUCKET"),
    )

    health_host: str = Field(
        default="0.0.0.0",
        validation_alias=AliasChoices("PIPEFORGE_WORKER_HEALTH_HOST", "WORKER_HEALTH_HOST"),
    )
    health_port: int = Field(
        default=8090,
        validation_alias=AliasChoices("PIPEFORGE_WORKER_HEALTH_PORT", "WORKER_HEALTH_PORT"),
    )
    heartbeat_interval_seconds: float = Field(
        default=15.0,
        validation_alias=AliasChoices(
            "PIPEFORGE_WORKER_HEARTBEAT_INTERVAL", "WORKER_HEARTBEAT_INTERVAL"
        ),
    )
    shutdown_timeout_seconds: float = Field(
        default=10.0,
        validation_alias=AliasChoices(
            "PIPEFORGE_WORKER_SHUTDOWN_TIMEOUT", "PIPEFORGE_SHUTDOWN_TIMEOUT"
        ),
    )
    health_only: bool = Field(
        default=False,
        validation_alias=AliasChoices("PIPEFORGE_WORKER_HEALTH_ONLY", "WORKER_HEALTH_ONLY"),
    )
    contracts_dir: Path = Field(
        default_factory=default_contracts_dir,
        validation_alias=AliasChoices("PIPEFORGE_CONTRACTS_DIR", "WORKER_CONTRACTS_DIR"),
    )

    @field_validator("supported_operations", mode="before")
    @classmethod
    def parse_operations(cls, value: Any) -> tuple[str, ...]:
        if isinstance(value, str):
            value = value.split(",")
        if not isinstance(value, (tuple, list, set)):
            raise ValueError("supported operations must be a comma-separated string or sequence")
        return tuple(str(item).strip() for item in value if str(item).strip())

    @field_validator("log_level")
    @classmethod
    def normalize_log_level(cls, value: str) -> str:
        normalized = value.strip().upper()
        if normalized not in {"DEBUG", "INFO", "WARNING", "ERROR", "CRITICAL"}:
            raise ValueError("log level is invalid")
        return normalized

    @field_validator(
        "instance_id",
        "hostname",
        "software_version",
        "broker_url",
        "broker_queue",
        "cancellation_queue",
        "dead_letter_exchange",
        "dead_letter_routing_key",
        "cancellation_dead_letter_routing_key",
        "commands_exchange",
        "events_exchange",
        "minio_endpoint",
        "minio_access_key",
        "minio_secret_key",
        "dataset_bucket",
        "artifact_bucket",
    )
    @classmethod
    def reject_blank_or_control_chars(cls, value: str) -> str:
        normalized = value.strip()
        if not normalized or any(ord(char) < 32 for char in normalized):
            raise ValueError(
                "configuration values must be non-empty and free of control characters"
            )
        return normalized

    @model_validator(mode="after")
    def validate_bounds(self) -> Settings:
        if not 1 <= self.max_concurrency <= 128:
            raise ValueError("max_concurrency must be between 1 and 128")
        if not 1 <= self.prefetch_count <= 1000:
            raise ValueError("prefetch_count must be between 1 and 1000")
        if not 1024 <= self.max_message_bytes <= 1 << 20:
            raise ValueError("max_message_bytes must be between 1024 and 1048576")
        if not 0.1 <= self.reconnect_delay_seconds <= 300:
            raise ValueError("reconnect_delay_seconds must be between 0.1 and 300")
        if not 1 <= self.health_port <= 65535:
            raise ValueError("health_port must be a valid TCP port")
        if not 1 <= self.heartbeat_interval_seconds <= 3600:
            raise ValueError("heartbeat_interval_seconds must be between 1 and 3600")
        if not 1 <= self.shutdown_timeout_seconds <= 300:
            raise ValueError("shutdown_timeout_seconds must be between 1 and 300")
        if not 1 <= len(self.supported_operations) <= 64:
            raise ValueError("supported_operations must contain between 1 and 64 entries")
        if len(set(self.supported_operations)) != len(self.supported_operations):
            raise ValueError("supported_operations must not contain duplicates")
        if any(len(operation) > 64 for operation in self.supported_operations):
            raise ValueError("supported operation names must be at most 64 characters")
        if not self.contracts_dir.is_dir() and not self.health_only:
            raise ValueError(f"contracts directory does not exist: {self.contracts_dir}")
        return self


class RedactedSettings(BaseModel):
    """Safe settings projection for diagnostics and structured logs."""

    worker_id: UUID
    instance_id: str
    hostname: str
    software_version: str
    max_concurrency: int
    prefetch_count: int
    broker_queue: str
    cancellation_queue: str
    health_host: str
    health_port: int
    health_only: bool


def redacted_settings(settings: Settings) -> RedactedSettings:
    return RedactedSettings(
        worker_id=settings.worker_id,
        instance_id=settings.instance_id,
        hostname=settings.hostname,
        software_version=settings.software_version,
        max_concurrency=settings.max_concurrency,
        prefetch_count=settings.prefetch_count,
        broker_queue=settings.broker_queue,
        cancellation_queue=settings.cancellation_queue,
        health_host=settings.health_host,
        health_port=settings.health_port,
        health_only=settings.health_only,
    )
