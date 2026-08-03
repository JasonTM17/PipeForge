"""Route each supported processing operation to its specialist aggregator."""

from __future__ import annotations

from pipeforge_worker.anomaly.dispatch import anomaly_operation_dispatcher
from pipeforge_worker.contracts.operations import OperationRequest, OperationType
from pipeforge_worker.processors.protocols import ChunkAggregator, OperationDispatcher
from pipeforge_worker.profilers.dispatch import profile_operation_dispatcher
from pipeforge_worker.validators.dispatch import quality_operation_dispatcher


class ProductionOperationDispatcher(OperationDispatcher):
    """Combine specialist dispatchers without leaking their implementation details."""

    def __init__(self) -> None:
        self._profiling = profile_operation_dispatcher()
        self._quality = quality_operation_dispatcher()
        self._anomaly = anomaly_operation_dispatcher()

    def build(self, operation: OperationRequest) -> ChunkAggregator:
        if operation.type is OperationType.PROFILE_DATASET:
            return self._profiling.build(operation)
        if operation.type in {
            OperationType.CHECK_MISSING_VALUES,
            OperationType.CHECK_DUPLICATES,
            OperationType.VALIDATE_QUALITY,
        }:
            return self._quality.build(operation)
        if operation.type is OperationType.DETECT_OUTLIERS:
            return self._anomaly.build(operation)
        raise ValueError(f"no production processor registered for {operation.type.value}")
