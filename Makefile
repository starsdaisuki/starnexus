SHELL := /bin/bash
.PHONY: build-server build-agent build-bot build-all clean

build-server:
	cd server && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ../bin/starnexus-server

build-agent:
	cd agent && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ../bin/starnexus-agent

build-bot:
	cd bot && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ../bin/starnexus-bot

build-all: build-server build-agent build-bot

clean:
	rm -rf bin/
