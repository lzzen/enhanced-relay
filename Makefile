# AI acceptance harness entry points (docs/ai-testing-acceptance.md §3.1).
# `make verify` is the single command CI and AI agents both run.
#
# Windows note: the race detector needs a C toolchain (CGO). On a Windows box
# without gcc, run the full race gate via Docker Desktop or WSL:
#     make verify-docker      # runs `make verify` inside golang:1.25 (Linux)
#     wsl make verify         # or run it in a WSL Ubuntu shell
# Locally on Windows without gcc, `make verify-fast` runs everything but race.

GO ?= go
DOCKER ?= docker
GO_IMAGE ?= golang:1.25

MUTATION_THRESHOLD ?= 80

.PHONY: verify verify-fast accept accept-fast verify-docker ci ci-docker \
        fmt vet build test test-race up down clean

## verify: full, hermetic acceptance gate (race + requirement traceability).
verify: fmt vet build accept
	@echo "verify: OK"

## verify-fast: quick inner-loop for AI iteration (no race).
verify-fast: vet build accept-fast
	@echo "verify-fast: OK"

## accept: run the acceptance gate with the race detector (canonical / CI).
accept:
	$(GO) run ./cmd/acceptance -race

## accept-fast: acceptance gate without race (local Windows without gcc).
accept-fast:
	$(GO) run ./cmd/acceptance

# Persistent caches so containerized runs download deps/tools only once.
# NOTE: intentionally NOT mounting the go-build cache (~/.cache/go-build): a
# stale build/test cache makes gremlins gather empty coverage for some packages
# (0 mutants -> false pass). Module + tool caches are network-only and safe.
DOCKER_CACHE = -v er-gomod:/go/pkg/mod -v er-gobin:/go/bin

## verify-docker: run the full race gate in a Linux container (Windows helper).
verify-docker:
	$(DOCKER) run --rm -e CGO_ENABLED=1 $(DOCKER_CACHE) -v "$(CURDIR)":/src -w /src $(GO_IMAGE) make verify

## ci: run the whole CI pipeline locally (gofmt + verify + mutation). Linux/WSL.
ci:
	MUTATION_THRESHOLD=$(MUTATION_THRESHOLD) bash scripts/ci.sh

## ci-docker: run the whole CI pipeline in a Linux container. GitHub-independent.
## Persistent volumes mean gremlins + modules download only on the first run.
ci-docker:
	$(DOCKER) run --rm -e CGO_ENABLED=1 -e MUTATION_THRESHOLD=$(MUTATION_THRESHOLD) \
		$(DOCKER_CACHE) -v "$(CURDIR)":/src -w /src $(GO_IMAGE) bash scripts/ci.sh

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

build:
	$(GO) build ./...

test:
	$(GO) test ./...

## test-race: raw race run (needs CGO). Prefer `make verify` / `verify-docker`.
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
	rm -rf bin dist coverage build
