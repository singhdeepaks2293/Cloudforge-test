# CloudForge — root Makefile (Milestone 9)
#
# Modelled on the GitHub MVP Makefile, adjusted to the local
# architecture (backend/ + rust-network-engine/ + frontend/ +
# deployments/compose/). `make verify` runs every local check and
# must pass without Docker; `up`/`down`/`logs`/`ps` need Docker and
# fail with a clear message when it is absent.
#
# Go toolchain: `go` from PATH when present, otherwise the Go
# toolchain in the module cache (this sandbox has no /usr/local/go).
# Override with e.g. `make verify GO=/usr/local/go/bin/go`.
# GOTOOLCHAIN=local keeps the build from downloading a toolchain.
SHELL := /bin/bash
# --env-file .env is required: `docker compose -f <subdir>/...` sets
# the project directory to the compose file's directory and reads
# .env from THERE, not from the repo root — without it the root .env
# is silently ignored and every ${...:?} variable reports missing
# (seen live on Fedora 2026-11-22). Always run make from the repo root.
COMPOSE := docker compose --env-file .env -f deployments/compose/docker-compose.yml
# Lightweight mode (classroom laptops): a separate compose file with
# the trimmed stack (no Prometheus/Grafana/MinIO, tuned-down
# Postgres/Redis, simulated networking, no KVM). `make up` is always
# the full mode; `make up-lite` is always lite. CLOUDFORGE_MODE in
# .env (lite|full, default full) is the in-app switch — the Makefile
# does not read it for `up`, so the two targets can never be confused.
COMPOSE_LITE := docker compose --env-file .env -f deployments/compose/docker-compose.light.yml
GO ?= $(shell command -v go 2>/dev/null || ls $(HOME)/go/pkg/mod/golang.org/toolchain@*/bin/go 2>/dev/null | sort | tail -n 1)
CARGO ?= $(shell command -v cargo 2>/dev/null || echo $(HOME)/.cargo/bin/cargo)
export GOTOOLCHAIN := local

.PHONY: help proto backend-build backend-vet backend-test engine-build engine-test frontend-install frontend-build frontend-test frontend-lint verify up down logs ps up-lite down-lite logs-lite ps-lite fmt clean check-go check-docker

help: ## List available targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-18s %s\n", $$1, $$2}'

check-go: ## Fail clearly when no Go toolchain can be found
	@test -n "$(GO)" -a -x "$(GO)" || { echo "error: Go toolchain not found. Install Go 1.25+ from https://go.dev/dl/ or set GO=/path/to/go"; exit 1; }

check-docker: ## Fail clearly when Docker is not installed
	@command -v docker >/dev/null 2>&1 || { echo "error: Docker is not installed in this environment. 'make up/down/logs/ps' need Docker on a real host."; exit 1; }

proto: check-go ## Regenerate Go and Rust protobuf stubs
	protoc -I proto \
		--go_out=backend --go_opt=module=github.com/cloudforge/cloudforge/backend \
		--go-grpc_out=backend --go-grpc_opt=module=github.com/cloudforge/cloudforge/backend \
		proto/cloudforge/v1/common.proto proto/cloudforge/v1/network_engine.proto
	cd rust-network-engine && $(CARGO) build -p cloudforge-grpc-server

backend-build: check-go ## Build the Go API and worker
	cd backend && $(GO) build ./...

backend-vet: check-go ## Vet the Go code
	cd backend && $(GO) vet ./...

backend-test: check-go ## Run Go tests (Postgres/Redis integration tests run when DATABASE_URL/REDIS_ADDR are reachable)
	cd backend && $(GO) test ./...

engine-build: ## Build the Rust engine (debug)
	cd rust-network-engine && $(CARGO) build --workspace

engine-test: ## Run Rust tests
	cd rust-network-engine && $(CARGO) test --workspace

frontend-install: ## Install frontend dependencies
	cd frontend && npm ci --no-audit --no-fund

frontend-build: ## Type-check and build the frontend
	cd frontend && npm run build

frontend-test: ## Run frontend unit tests
	cd frontend && npm test

frontend-lint: ## Lint the frontend
	cd frontend && npm run lint

verify: backend-vet backend-test engine-test frontend-build frontend-lint frontend-test ## Run every local check (no Docker needed)

up: check-docker ## Build and start the full stack (full mode)
	$(COMPOSE) up -d --build

down: check-docker ## Stop the stack (volumes are kept)
	$(COMPOSE) down

logs: check-docker ## Follow stack logs
	$(COMPOSE) logs -f

ps: check-docker ## Show stack status
	$(COMPOSE) ps

up-lite: check-docker ## Build and start the lightweight stack (classroom laptops)
	$(COMPOSE_LITE) up -d --build

down-lite: check-docker ## Stop the lightweight stack (volumes are kept)
	$(COMPOSE_LITE) down

logs-lite: check-docker ## Follow lightweight stack logs
	$(COMPOSE_LITE) logs -f

ps-lite: check-docker ## Show lightweight stack status
	$(COMPOSE_LITE) ps

fmt: check-go ## Format Go and Rust sources
	cd backend && $(GO)fmt -w .
	cd rust-network-engine && $(CARGO) fmt --all

clean: ## Remove local build output
	rm -rf frontend/dist backend/bin
	cd rust-network-engine && $(CARGO) clean
