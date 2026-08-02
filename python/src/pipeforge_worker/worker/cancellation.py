"""In-memory cancellation registry used at safe worker boundaries."""

from __future__ import annotations

from dataclasses import dataclass
from datetime import UTC, datetime
from threading import RLock
from uuid import UUID


@dataclass(frozen=True, slots=True)
class CancellationRequest:
    job_id: UUID
    attempt_id: UUID | None
    lease_id: UUID | None
    reason: str
    requested_at: datetime


class CancellationRegistry:
    """Keep bounded cancellation intent without letting workers mutate Go state."""

    def __init__(self, max_entries: int = 10_000) -> None:
        if not 1 <= max_entries <= 100_000:
            raise ValueError("max_entries is outside the supported bound")
        self._max_entries = max_entries
        self._requests: dict[UUID, CancellationRequest] = {}
        self._lock = RLock()

    def request(
        self,
        job_id: UUID,
        reason: str,
        attempt_id: UUID | None = None,
        lease_id: UUID | None = None,
    ) -> CancellationRequest:
        if job_id.int == 0:
            raise ValueError("job_id is required")
        if attempt_id is not None and attempt_id.int == 0:
            raise ValueError("attempt_id must be non-zero")
        if lease_id is not None and lease_id.int == 0:
            raise ValueError("lease_id must be non-zero")
        if lease_id is not None and attempt_id is None:
            raise ValueError("lease_id requires attempt_id")
        reason = reason.strip()
        if not 1 <= len(reason) <= 500:
            raise ValueError("cancellation reason must contain 1-500 characters")
        request = CancellationRequest(job_id, attempt_id, lease_id, reason, datetime.now(UTC))
        with self._lock:
            if job_id not in self._requests and len(self._requests) >= self._max_entries:
                oldest_job = min(self._requests, key=lambda key: self._requests[key].requested_at)
                del self._requests[oldest_job]
            self._requests[job_id] = request
        return request

    def is_cancelled(
        self,
        job_id: UUID,
        attempt_id: UUID | None = None,
        lease_id: UUID | None = None,
    ) -> bool:
        with self._lock:
            request = self._requests.get(job_id)
        if request is None:
            return False
        if request.attempt_id is not None and request.attempt_id != attempt_id:
            return False
        if request.lease_id is not None and request.lease_id != lease_id:
            return False
        return True

    def reason_for(
        self,
        job_id: UUID,
        attempt_id: UUID | None = None,
        lease_id: UUID | None = None,
    ) -> str | None:
        with self._lock:
            request = self._requests.get(job_id)
        if request is None:
            return None
        if request.attempt_id is not None and request.attempt_id != attempt_id:
            return None
        if request.lease_id is not None and request.lease_id != lease_id:
            return None
        return request.reason

    def clear(self, job_id: UUID) -> None:
        with self._lock:
            self._requests.pop(job_id, None)

    def clear_if_matches(
        self,
        job_id: UUID,
        attempt_id: UUID | None = None,
        lease_id: UUID | None = None,
    ) -> bool:
        with self._lock:
            request = self._requests.get(job_id)
            if request is None:
                return False
            if request.attempt_id is not None and request.attempt_id != attempt_id:
                return False
            if request.lease_id is not None and request.lease_id != lease_id:
                return False
            del self._requests[job_id]
            return True

    def snapshot(self) -> tuple[CancellationRequest, ...]:
        with self._lock:
            return tuple(self._requests.values())
