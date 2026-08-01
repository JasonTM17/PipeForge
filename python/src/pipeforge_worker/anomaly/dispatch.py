"""Operation dispatcher for anomaly detection."""

from __future__ import annotations

from collections.abc import Callable

from pipeforge_worker.anomaly.aggregator import AnomalyAggregator
from pipeforge_worker.anomaly.config import AnomalyConfig
from pipeforge_worker.contracts.operations import OperationRequest, OperationType
from pipeforge_worker.processors.protocols import MappingOperationDispatcher


def anomaly_operation_dispatcher(
    *, monotonic: Callable[[], float] | None = None
) -> MappingOperationDispatcher:
    def build(operation: OperationRequest) -> AnomalyAggregator:
        config = AnomalyConfig.from_mapping(operation.config)
        return AnomalyAggregator(config, monotonic=monotonic)

    return MappingOperationDispatcher({OperationType.DETECT_OUTLIERS: build})
