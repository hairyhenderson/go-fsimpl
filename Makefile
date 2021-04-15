.DEFAULT_GOAL = test

ifeq ("$(CI)","true")
LINT_PROCS ?= 1
else
LINT_PROCS ?= $(shell nproc)
endif

ifeq ($(OS),Windows_NT)
test:
	go test -coverprofile=c.out ./...
else
test:
	go test -race -coverprofile=c.out ./...
endif

lint:
	@golangci-lint run --verbose --max-same-issues=0 --max-issues-per-linter=0

ci-lint:
	@golangci-lint run --verbose --max-same-issues=0 --max-issues-per-linter=0 --out-format=github-actions

.PHONY: test lint ci-lint
.DELETE_ON_ERROR:
.SECONDARY:
