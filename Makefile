SHELL := /bin/bash
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
SERVER_LDFLAGS := -X github.com/starsdaisuki/starnexus/server/internal/buildinfo.Version=$(VERSION) -X github.com/starsdaisuki/starnexus/server/internal/buildinfo.Commit=$(COMMIT) -X github.com/starsdaisuki/starnexus/server/internal/buildinfo.BuildTime=$(BUILD_TIME)
AGENT_LDFLAGS := -X github.com/starsdaisuki/starnexus/agent/internal/buildinfo.Version=$(VERSION) -X github.com/starsdaisuki/starnexus/agent/internal/buildinfo.Commit=$(COMMIT) -X github.com/starsdaisuki/starnexus/agent/internal/buildinfo.BuildTime=$(BUILD_TIME)
BOT_LDFLAGS := -X github.com/starsdaisuki/starnexus/bot/internal/buildinfo.Version=$(VERSION) -X github.com/starsdaisuki/starnexus/bot/internal/buildinfo.Commit=$(COMMIT) -X github.com/starsdaisuki/starnexus/bot/internal/buildinfo.BuildTime=$(BUILD_TIME)
.PHONY: build-server build-agent build-bot build-analyze build-bench build-loadtest build-all test check analyze bench figures loadtest export-analysis clean

build-server:
	cd server && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(SERVER_LDFLAGS)" -o ../bin/starnexus-server

build-agent:
	cd agent && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(AGENT_LDFLAGS)" -o ../bin/starnexus-agent

build-bot:
	cd bot && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(BOT_LDFLAGS)" -o ../bin/starnexus-bot

build-analyze:
	cd server && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ../bin/starnexus-analyze ./cmd/starnexus-analyze

build-bench:
	cd server && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ../bin/starnexus-bench ./cmd/starnexus-bench

build-loadtest:
	cd server && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ../bin/starnexus-loadtest ./cmd/starnexus-loadtest

build-all: build-server build-agent build-bot build-analyze build-bench build-loadtest

test:
	cd server && go test ./...
	cd agent && go test ./...
	cd bot && go test ./...

check: test
	@if command -v shellcheck >/dev/null 2>&1; then \
	  shellcheck -S warning scripts/*.sh; \
	else \
	  echo "shellcheck not installed — falling back to bash syntax check"; \
	  bash -n scripts/*.sh; \
	fi
	cd web && pnpm exec tsc --noEmit
	cd web && pnpm audit --audit-level moderate

analyze:
	cd server && go run ./cmd/starnexus-analyze -db ./starnexus.db -schema ./schema.sql -out ../analysis-output -hours 168

bench:
	cd server && go run ./cmd/starnexus-bench -db ./starnexus.db -schema ./schema.sql -out ../analysis-output/bench -experiments ../analysis-output/experiments.jsonl -hours 168

figures:
	uv run scripts/generate-figures.py

loadtest:
	scripts/loadtest-local.sh

export-analysis:
	scripts/export-analysis.sh

clean:
	rm -rf bin/ analysis-output/
