"""Small deterministic aggregators used by pipeline tests and composition."""

from __future__ import annotations

from collections.abc import Mapping

from pipeforge_worker.readers.models import DataChunk, ReaderMetadata


class RowCountAggregator:
    """Count rows and expose schema metadata without claiming a domain operation."""

    def __init__(self) -> None:
        self._metadata: ReaderMetadata | None = None
        self._rows = 0
        self._chunks = 0

    def start(self, metadata: ReaderMetadata) -> None:
        self._metadata = metadata

    def consume(self, chunk: DataChunk) -> None:
        self._rows += chunk.frame.height
        self._chunks += 1

    def finish(self) -> Mapping[str, object]:
        if self._metadata is None:
            raise RuntimeError("aggregator was not started")
        return {
            "rowsProcessed": self._rows,
            "chunksProcessed": self._chunks,
            "columns": list(self._metadata.columns),
        }
