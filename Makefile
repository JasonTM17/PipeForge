SHELL := /bin/sh

PLAN ?= plans/20260801-1300-pipeforge-platform/plan.md

.PHONY: help plan-status

help:
	@printf '%s\n' 'PipeForge foundation targets:'
	@printf '%s\n' '  make plan-status  Show the active CK implementation plan'
	@printf '%s\n' '  make help         Show this message'

plan-status:
	ck plan status "$(PLAN)"

