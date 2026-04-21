SHELL := /bin/bash
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
SERVER_LDFLAGS := -X github.com/starsdaisuki/starnexus/server/internal/buildinfo.Version=$(VERSION) -X github.com/starsdaisuki/starnexus/server/internal/buildinfo.Commit=$(COMMIT) -X github.com/starsdaisuki/starnexus/server/internal/buildinfo.BuildTime=$(BUILD_TIME)
AGENT_LDFLAGS := -X github.com/starsdaisuki/starnexus/agent/internal/buildinfo.Version=$(VERSION) -X github.com/starsdaisuki/starnexus/agent/internal/buildinfo.Commit=$(COMMIT) -X github.com/starsdaisuki/starnexus/agent/internal/buildinfo.BuildTime=$(BUILD_TIME)
BOT_LDFLAGS := -X github.com/starsdaisuki/starnexus/bot/internal/buildinfo.Version=$(VERSION) -X github.com/starsdaisuki/starnexus/bot/internal/buildinfo.Commit=$(COMMIT) -X github.com/starsdaisuki/starnexus/bot/internal/buildinfo.BuildTime=$(BUILD_TIME)
.PHONY: build-server build-agent build-bot build-analyze build-all analyze clean

build-server:
	cd server && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(SERVER_LDFLAGS)" -o ../bin/starnexus-server

build-agent:
	cd agent && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(AGENT_LDFLAGS)" -o ../bin/starnexus-agent

build-bot:
	cd bot && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(BOT_LDFLAGS)" -o ../bin/starnexus-bot

build-analyze:
	cd server && go build -o ../bin/starnexus-analyze ./cmd/starnexus-analyze

build-all: build-server build-agent build-bot build-analyze

analyze:
	cd server && go run ./cmd/starnexus-analyze -db ./starnexus.db -schema ./schema.sql -out ../analysis-output -hours 168

clean:
	rm -rf bin/ analysis-output/
