SHELL := /bin/bash
.PHONY: build-server build-agent build-bot build-analyze build-all analyze clean

build-server:
	cd server && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ../bin/starnexus-server

build-agent:
	cd agent && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ../bin/starnexus-agent

build-bot:
	cd bot && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ../bin/starnexus-bot

build-analyze:
	cd server && go build -o ../bin/starnexus-analyze ./cmd/starnexus-analyze

build-all: build-server build-agent build-bot build-analyze

analyze:
	cd server && go run ./cmd/starnexus-analyze -db ./starnexus.db -schema ./schema.sql -out ../analysis-output -hours 168

clean:
	rm -rf bin/ analysis-output/
