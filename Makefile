SHELL := /bin/sh

PLAN ?= plans/20260801-1300-pipeforge-platform/plan.md

.PHONY: help plan-status validate-env bootstrap migrate dev e2e worker-test worker-lint worker-health cleanup down

help:
	@printf '%s\n' 'PipeForge development targets:'
	@printf '%s\n' '  make bootstrap    Validate .env and start dependency services'
	@printf '%s\n' '  make validate-env Validate required local environment values'
	@printf '%s\n' '  make migrate     Apply database migrations through the Compose tool service'
	@printf '%s\n' '  make dev          Start the current Compose stack'
	@printf '%s\n' '  make e2e          Run the disposable API-to-artifact Compose smoke test'
	@printf '%s\n' '  make worker-test  Run Python worker tests and type checks'
	@printf '%s\n' '  make worker-lint  Run Python worker lint and format checks'
	@printf '%s\n' '  make worker-health Start an explicit health-only worker'
	@printf '%s\n' '  make cleanup      Run one bounded expired multipart cleanup batch'
	@printf '%s\n' '  make down         Stop PipeForge Compose services'
	@printf '%s\n' '  make plan-status  Show the active CK implementation plan'
	@printf '%s\n' '  make help         Show this message'

plan-status:
	ck plan status "$(PLAN)"

validate-env:
	@powershell -NoProfile -ExecutionPolicy Bypass -File scripts/validate-env.ps1 -Path .env

bootstrap:
	@powershell -NoProfile -ExecutionPolicy Bypass -File scripts/bootstrap.ps1

migrate: validate-env
	docker compose --profile tools run --rm migrate

dev: validate-env
	docker compose up --build

e2e: validate-env
	@powershell -NoProfile -ExecutionPolicy Bypass -File scripts/e2e-compose.ps1

worker-test:
	python/.venv/Scripts/python.exe -m pytest python/tests
	python/.venv/Scripts/python.exe -m mypy python/src

worker-lint:
	python/.venv/Scripts/ruff.exe check python/src python/tests
	python/.venv/Scripts/ruff.exe format --check python/src python/tests

worker-health: validate-env
	PIPEFORGE_WORKER_HEALTH_ONLY=true docker compose --env-file .env up --build -d worker

cleanup: validate-env
	docker compose --profile jobs run --rm scheduler

down:
	docker compose down
