"""Operation dispatcher for the profiling operation."""

from __future__ import annotations

from collections.abc import Callable

from pipeforge_worker.contracts.operations import OperationRequest, OperationType
from pipeforge_worker.processors.protocols import MappingOperationDispatcher
from pipeforge_worker.profilers.aggregator import ProfileAggregator
from pipeforge_worker.profilers.config import ProfileConfig


def profile_operation_dispatcher(
    *, monotonic: Callable[[], float] | None = None
) -> MappingOperationDispatcher:
    def build(operation: OperationRequest) -> ProfileAggregator:
        config = ProfileConfig.from_mapping(operation.config)
        if monotonic is None:
            return ProfileAggregator(config)
        return ProfileAggregator(config, monotonic=monotonic)

    return MappingOperationDispatcher({OperationType.PROFILE_DATASET: build})
