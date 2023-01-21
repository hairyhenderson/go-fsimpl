.DEFAULT_GOAL = test

ifeq ("$(CI)","true")
LINT_PROCS ?= 1
else
LINT_PROCS ?= $(shell nproc)
endif

test:
	CGO_ENABLED=0 go test -coverprofile=c.out ./...

test-race:
	go test -race -coverprofile=c.out ./...

lint:
	@golangci-lint run --verbose --max-same-issues=0 --max-issues-per-linter=0

ci-lint:
	@golangci-lint run --verbose --max-same-issues=0 --max-issues-per-linter=0 --out-format=github-actions

.PHONY: test lint ci-lint
.DELETE_ON_ERROR:
.SECONDARY:
