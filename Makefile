.DEFAULT_GOAL = test

extension = $(patsubst windows,.exe,$(filter windows,$(1)))
GOOS ?= $(shell go version | sed 's/^.*\ \([a-z0-9]*\)\/\([a-z0-9]*\)/\1/')
GOARCH ?= $(shell go version | sed 's/^.*\ \([a-z0-9]*\)\/\([a-z0-9]*\)/\2/')

ifeq ("$(TARGETVARIANT)","")
ifneq ("$(GOARM)","")
TARGETVARIANT := v$(GOARM)
endif
else
ifeq ("$(GOARM)","")
GOARM ?= $(subst v,,$(TARGETVARIANT))
endif
endif

ifeq ("$(CI)","true")
LINT_PROCS ?= 1
else
LINT_PROCS ?= $(shell nproc)
endif

test:
	CGO_ENABLED=0 go test -coverprofile=c.out ./...

test-race:
	go test -race -coverprofile=c.out ./...

bin/fscli_%v7$(call extension,$(GOOS)): $(shell find . -type f -name "*.go")
	GOOS=$(shell echo $* | cut -f1 -d-) \
	GOARCH=$(shell echo $* | cut -f2 -d- ) \
	GOARM=7 \
	CGO_ENABLED=0 \
		go build $(BUILD_ARGS) -o $@ ./examples/fscli

bin/fscli_windows-%.exe: $(shell find . -type f -name "*.go")
	GOOS=windows \
	GOARCH=$* \
	GOARM= \
	CGO_ENABLED=0 \
		go build $(BUILD_ARGS) -o $@ ./examples/fscli

bin/fscli_%$(TARGETVARIANT)$(call extension,$(GOOS)): $(shell find . -type f -name "*.go")
	GOOS=$(shell echo $* | cut -f1 -d-) \
	GOARCH=$(shell echo $* | cut -f2 -d- ) \
	GOARM=$(GOARM) \
	CGO_ENABLED=0 \
		go build $(BUILD_ARGS) -o $@ ./examples/fscli

lint:
	@golangci-lint run --verbose --max-same-issues=0 --max-issues-per-linter=0

ci-lint:
	@golangci-lint run --verbose --max-same-issues=0 --max-issues-per-linter=0 --out-format=github-actions

ifneq ("$(VERSION)","")
release:
	git tag -sm "Releasing v$(VERSION)" v$(VERSION) && git push --tags
	gh release create v$(VERSION) --draft --generate-notes
endif

.PHONY: test lint ci-lint
.DELETE_ON_ERROR:
.SECONDARY:
