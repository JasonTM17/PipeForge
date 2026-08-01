"""CLI entry point with portable signal handling."""

from __future__ import annotations

import argparse
import asyncio
import logging
import signal
from collections.abc import Sequence

from pydantic import ValidationError

from pipeforge_worker.app import build_runtime
from pipeforge_worker.config import Settings, redacted_settings
from pipeforge_worker.observability.logging import configure_logging, log_event


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Run the PipeForge Python worker")
    parser.add_argument(
        "--health-only",
        action="store_true",
        help="serve health/metrics without broker or storage intake",
    )
    args = parser.parse_args(argv)
    try:
        settings = Settings(health_only=True) if args.health_only else Settings()
    except ValidationError as exc:
        print(exc, flush=True)
        return 2
    configure_logging(settings.log_level)
    log_event(
        logging.getLogger(__name__),
        logging.INFO,
        "worker_configured",
        **redacted_settings(settings).model_dump(mode="json"),
    )
    try:
        asyncio.run(_run(settings))
    except KeyboardInterrupt:
        return 0
    return 0


async def _run(settings: Settings) -> None:
    stop_event = asyncio.Event()
    loop = asyncio.get_running_loop()
    _install_signal_handlers(loop, stop_event)
    runtime = build_runtime(settings)
    await runtime.run(stop_event)


def _install_signal_handlers(loop: asyncio.AbstractEventLoop, stop_event: asyncio.Event) -> None:
    for signum in (signal.SIGINT, signal.SIGTERM):
        try:
            loop.add_signal_handler(signum, stop_event.set)
        except (NotImplementedError, RuntimeError):
            signal.signal(signum, lambda _signum, _frame: loop.call_soon_threadsafe(stop_event.set))


if __name__ == "__main__":
    raise SystemExit(main())
