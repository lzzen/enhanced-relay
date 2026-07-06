# AI acceptance harness entry points (docs/ai-testing-acceptance.md §3.1).
# `make verify` is the single command CI and AI agents both run.

GO ?= go

.PHONY: verify verify-fast fmt vet build test test-race up down clean

## verify: full, hermetic, deterministic acceptance run.
verify: fmt vet build test-race
	@echo "verify: OK"

## verify-fast: quick inner-loop subset for AI iteration (no race).
verify-fast: vet test
	@echo "verify-fast: OK"

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

build:
	$(GO) build ./...

test:
	$(GO) test ./...

## test-race: race detector gate (canonical, runs in Linux CI).
## Requires a C toolchain + CGO_ENABLED=1. On a Windows box without gcc,
## use `make verify-fast` locally; CI still enforces race on every PR.
test-race:
	CGO_ENABLED=1 $(GO) test -race ./...

## up: bring up hermetic dependencies (postgres/redis/minio/toxiproxy).
## Placeholder until the container harness lands; core verify needs no daemons.
up:
	@echo "up: no external dependencies required yet (Phase -1 skeleton)"

down:
	@echo "down: nothing to tear down yet"

clean:
	$(GO) clean
	rm -rf bin dist coverage
