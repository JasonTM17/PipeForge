"""Thread-safe health state and a dependency-free HTTP endpoint server."""

from __future__ import annotations

import json
import logging
import threading
from collections.abc import Mapping
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any

from pipeforge_worker.observability.metrics import WorkerMetrics


class HealthState:
    def __init__(self, max_concurrency: int) -> None:
        self._lock = threading.Lock()
        self._max_concurrency = max_concurrency
        self._status = "STARTING"
        self._current_jobs: tuple[str, ...] = ()
        self._dependencies: dict[str, bool] = {}

    def set_status(self, status: str) -> None:
        with self._lock:
            self._status = status

    def set_current_jobs(self, job_ids: list[str] | tuple[str, ...]) -> None:
        with self._lock:
            self._current_jobs = tuple(sorted(set(job_ids))[:128])

    def set_dependency(self, name: str, healthy: bool) -> None:
        with self._lock:
            self._dependencies[name] = healthy

    def snapshot(self) -> dict[str, Any]:
        with self._lock:
            dependencies = dict(self._dependencies)
            status = self._status
            current_jobs = self._current_jobs
            max_concurrency = self._max_concurrency
        return {
            "status": status,
            "currentConcurrency": len(current_jobs),
            "maxConcurrency": max_concurrency,
            "dependencies": dependencies,
        }

    def is_ready(self) -> bool:
        snapshot = self.snapshot()
        return snapshot["status"] in {"READY", "BUSY", "DRAINING"} and all(
            snapshot["dependencies"].values()
        )


class HealthServer:
    def __init__(self, state: HealthState, metrics: WorkerMetrics, host: str, port: int) -> None:
        self._state = state
        self._metrics = metrics
        self._host = host
        self._port = port
        self._server: ThreadingHTTPServer | None = None
        self._thread: threading.Thread | None = None
        self._logger = logging.getLogger(__name__)

    def start(self) -> None:
        state = self._state
        metrics = self._metrics

        class Handler(BaseHTTPRequestHandler):
            def do_GET(self) -> None:
                if self.path == "/health/live":
                    _write_json(self, 200, {"status": "alive"})
                    return
                if self.path == "/health/ready":
                    status = 200 if state.is_ready() else 503
                    _write_json(self, status, state.snapshot())
                    return
                if self.path == "/metrics":
                    body = metrics.render()
                    self.send_response(200)
                    self.send_header("Content-Type", "text/plain; version=0.0.4")
                    self.send_header("Content-Length", str(len(body)))
                    self.end_headers()
                    self.wfile.write(body)
                    return
                _write_json(self, 404, {"status": "not_found"})

            def log_message(self, _format: str, *_args: object) -> None:
                return

        self._server = ThreadingHTTPServer((self._host, self._port), Handler)
        self._thread = threading.Thread(
            target=self._server.serve_forever, name="worker-health", daemon=True
        )
        self._thread.start()
        self._logger.info("worker health server started on %s:%d", self._host, self._port)

    def stop(self) -> None:
        if self._server is None:
            return
        self._server.shutdown()
        self._server.server_close()
        if self._thread is not None:
            self._thread.join(timeout=5)
        self._server = None
        self._thread = None


def _write_json(handler: BaseHTTPRequestHandler, status: int, payload: Mapping[str, Any]) -> None:
    body = json.dumps(payload, separators=(",", ":")).encode("utf-8")
    handler.send_response(status)
    handler.send_header("Content-Type", "application/json")
    handler.send_header("Content-Length", str(len(body)))
    handler.end_headers()
    handler.wfile.write(body)
