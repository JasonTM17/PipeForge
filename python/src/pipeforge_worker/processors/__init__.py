"""Dependency-injected chunk processing orchestration."""

from pipeforge_worker.processors.pipeline import ProcessingPipeline
from pipeforge_worker.processors.protocols import (
    CancellationRequested,
    ChunkAggregator,
    MappingOperationDispatcher,
    ProcessingContext,
    ProcessingResult,
    ProgressSnapshot,
)

__all__ = [
    "CancellationRequested",
    "ChunkAggregator",
    "MappingOperationDispatcher",
    "ProcessingContext",
    "ProcessingPipeline",
    "ProcessingResult",
    "ProgressSnapshot",
]
