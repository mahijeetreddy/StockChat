# StockChat developer commands. Requires: go, node/npm, golangci-lint (for lint), docker (for up).
SHELL := /bin/bash
BACKEND := backend
FRONTEND := frontend
RACE ?= -race

.PHONY: dev dev-api dev-web test test-backend test-frontend lint lint-backend lint-frontend \
        eval build build-backend build-frontend up down install

install:
	cd $(FRONTEND) && npm ci
	cd $(BACKEND) && go mod download

## dev: run backend and frontend dev servers together
dev:
	$(MAKE) -j2 dev-api dev-web

dev-api:
	cd $(BACKEND) && go run ./cmd/server

dev-web:
	cd $(FRONTEND) && npm run dev

## test: backend + frontend tests (set RACE= to skip the race detector when CGO is unavailable)
test: test-backend test-frontend

test-backend:
	cd $(BACKEND) && go test $(RACE) ./...

test-frontend:
	cd $(FRONTEND) && npm test

lint: lint-backend lint-frontend

lint-backend:
	cd $(BACKEND) && go vet ./... && golangci-lint run ./...

lint-frontend:
	cd $(FRONTEND) && npm run lint

## eval: run the LLM eval suite (real Gemini API + mock market data; costs quota)
eval:
	cd $(BACKEND) && go test -tags eval -count=1 -timeout 30m -v ./evals/...

build: build-backend build-frontend

build-backend:
	cd $(BACKEND) && CGO_ENABLED=0 go build -o bin/server ./cmd/server

build-frontend:
	cd $(FRONTEND) && npm run build

## up: run the whole stack with Docker Compose (mock mode by default)
up:
	docker compose up --build

down:
	docker compose down
