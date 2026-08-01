import json
from pathlib import Path

import pytest

from pipeforge_worker.contracts.envelope import Envelope
from pipeforge_worker.contracts.validator import ContractValidationError, ContractValidator


@pytest.fixture()
def validator() -> ContractValidator:
    schema_dir = Path(__file__).resolve().parents[2] / "contracts" / "json-schema"
    return ContractValidator(schema_dir)


def test_validator_accepts_shared_worker_heartbeat(validator: ContractValidator) -> None:
    path = (
        Path(__file__).resolve().parents[2]
        / "contracts"
        / "examples"
        / "processing.worker.heartbeat.v1.json"
    )
    value = validator.validate_bytes(path.read_bytes())
    envelope = Envelope.from_mapping(value)
    assert envelope.message_type == "processing.worker.heartbeat"
    assert envelope.payload["currentConcurrency"] == 1


def test_validator_rejects_duplicate_keys_and_unsupported_operation(
    validator: ContractValidator,
) -> None:
    duplicate = (
        b'{"messageType":"processing.worker.heartbeat","messageType":"processing.worker.heartbeat"}'
    )
    with pytest.raises(ContractValidationError, match="duplicate JSON key"):
        validator.validate_bytes(duplicate)

    path = (
        Path(__file__).resolve().parents[2]
        / "contracts"
        / "examples"
        / "invalid"
        / "processing.job.requested.v1-invalid-unsupported-operation.json"
    )
    with pytest.raises(ContractValidationError):
        validator.validate_bytes(path.read_bytes())


def test_envelope_serialization_is_bounded_and_deterministic(validator: ContractValidator) -> None:
    message = json.loads(
        (
            Path(__file__).resolve().parents[2]
            / "contracts"
            / "examples"
            / "processing.worker.registered.v1.json"
        ).read_text()
    )
    envelope = Envelope.from_mapping(validator.validate_mapping(message))
    assert envelope.to_bytes() == envelope.to_bytes()
    assert len(envelope.to_bytes()) < 1 << 20
