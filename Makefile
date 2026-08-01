SHELL := /bin/sh

PLAN ?= plans/20260801-1300-pipeforge-platform/plan.md

.PHONY: help plan-status validate-env bootstrap migrate dev down

help:
	@printf '%s\n' 'PipeForge development targets:'
	@printf '%s\n' '  make bootstrap    Validate .env and start dependency services'
	@printf '%s\n' '  make validate-env Validate required local environment values'
	@printf '%s\n' '  make migrate     Apply database migrations through the Compose tool service'
	@printf '%s\n' '  make dev          Start the current Compose stack'
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

down:
	docker compose down
