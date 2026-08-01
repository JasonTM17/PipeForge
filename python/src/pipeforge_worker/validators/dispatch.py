"""Operation builders for quality-rule evaluation."""

from __future__ import annotations

from collections.abc import Callable, Mapping

from pipeforge_worker.contracts.operations import OperationRequest, OperationType
from pipeforge_worker.contracts.quality import parse_quality_rules
from pipeforge_worker.processors.protocols import MappingOperationDispatcher
from pipeforge_worker.validators.engine import QualityAggregator


def quality_operation_dispatcher(
    *, monotonic: Callable[[], float] | None = None
) -> MappingOperationDispatcher:
    def build(operation: OperationRequest) -> QualityAggregator:
        rules = parse_quality_rules(operation.config["rules"])
        return QualityAggregator(rules, monotonic=monotonic)

    def build_missing(operation: OperationRequest) -> QualityAggregator:
        columns = operation.config["columns"]
        assert isinstance(columns, list)
        rules = parse_quality_rules(
            [
                {
                    "id": f"missing-{column}",
                    "name": f"{column} must not be null",
                    "columnScope": [column],
                    "ruleType": "NOT_NULL",
                    "configuration": {},
                    "severity": "ERROR",
                    "enabled": True,
                }
                for column in columns
            ]
        )
        return QualityAggregator(rules, monotonic=monotonic)

    def build_duplicates(operation: OperationRequest) -> QualityAggregator:
        columns = operation.config["keyColumns"]
        assert isinstance(columns, list)
        rules = parse_quality_rules(
            [
                {
                    "id": "duplicate-key",
                    "name": "key columns must be unique",
                    "columnScope": columns,
                    "ruleType": "UNIQUE",
                    "configuration": {},
                    "severity": "ERROR",
                    "enabled": True,
                }
            ]
        )
        return QualityAggregator(rules, monotonic=monotonic)

    builders: Mapping[OperationType, Callable[[OperationRequest], QualityAggregator]] = {
        OperationType.VALIDATE_QUALITY: build,
        OperationType.CHECK_MISSING_VALUES: build_missing,
        OperationType.CHECK_DUPLICATES: build_duplicates,
    }
    return MappingOperationDispatcher(builders)
