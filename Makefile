# Network Test API Makefile
# =========================

.PHONY: help setup build run dev kill test test-bg bench clean docker-build docker-run fastly-build fastly-deploy fastly-logs check install all test-unit test-integration test-functional test-e2e test-acceptance test-all test-coverage lint

# Variables
BINARY := main
PORT := 8080
VERSION := 2.2.0

# Colors
GREEN  := $(shell tput -Txterm setaf 2)
YELLOW := $(shell tput -Txterm setaf 3)
BLUE   := $(shell tput -Txterm setaf 4)
RESET  := $(shell tput -Txterm sgr0)

help: ## Show this help
	@echo "$(BLUE)Network Test API - Available targets:$(RESET)"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  $(GREEN)%-15s$(RESET) %s\n", $$1, $$2}'

setup: ## Clean + install deps + build
	@echo "$(YELLOW)🚀 Setting up...$(RESET)"
	@rm -rf go.mod go.sum $(BINARY)
	@go mod init network-test-api
	@go get github.com/gorilla/mux@latest
	@go get github.com/tcaine/twamp@master
	@go mod tidy
	@$(MAKE) build
	@echo "$(GREEN)✅ Setup complete$(RESET)"

build: ## Build binary
	@echo "$(YELLOW)🔨 Building...$(RESET)"
	@CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BINARY) .
	@echo "$(GREEN)✅ Build complete: ./$(BINARY)$(RESET)"

run: kill ## Run server
	@echo "$(YELLOW)🚀 Starting on :$(PORT)...$(RESET)"
	@./$(BINARY)

dev: build run ## Build + run

kill: ## Stop server
	@-pkill -f "./$(BINARY)" 2>/dev/null || true
	@-lsof -ti:$(PORT) | xargs kill -9 2>/dev/null || true

clean: kill ## Clean all
	@echo "$(YELLOW)🧹 Cleaning...$(RESET)"
	@rm -rf $(BINARY) go.mod go.sum fastly.toml pkg bin
	@echo "$(GREEN)✅ Clean complete$(RESET)"

test: ## Run tests (server must be running)
	@echo "$(YELLOW)🧪 Running tests...$(RESET)"
	@sleep 1
	@echo "\n$(BLUE)Health Check$(RESET)"
	@curl -s localhost:$(PORT)/ | jq -r '.status'
	@echo "\n$(BLUE)TCP Test$(RESET)"
	@curl -s -X POST localhost:$(PORT)/iperf/client/run -H "Content-Type: application/json" -d '{"server_host":"iperf.he.net","duration":3,"protocol":"TCP"}' | jq '.data | {bandwidth_mbps, sent_bytes, duration_sec}'
	@echo "\n$(BLUE)UDP Test$(RESET)"
	@curl -s -X POST localhost:$(PORT)/iperf/client/run -H "Content-Type: application/json" -d '{"server_host":"iperf.he.net","duration":3,"protocol":"UDP"}' | jq '.data | {bandwidth_mbps, packets_sent, duration_sec}'
	@echo "$(GREEN)✅ Tests complete$(RESET)"

test-bg: ## Auto test (start + test + stop)
	@$(MAKE) kill
	@./$(BINARY) > /dev/null 2>&1 &
	@sleep 2
	@$(MAKE) test
	@$(MAKE) kill

bench: ## 10s benchmark
	@echo "$(YELLOW)📊 10s benchmark...$(RESET)"
	@curl -s -X POST localhost:$(PORT)/iperf/client/run -H "Content-Type: application/json" -d '{"server_host":"iperf.he.net","duration":10}' | jq '.data'

check: ## Health check
	@curl -s localhost:$(PORT)/health | jq || echo "$(YELLOW)Not running$(RESET)"

docker-build: ## Build Docker image
	@echo "$(YELLOW)🐳 Building Docker...$(RESET)"
	@docker build -t network-test-api:$(VERSION) .
	@echo "$(GREEN)✅ Image built$(RESET)"

docker-run: ## Run Docker container
	@docker run --rm -p $(PORT):$(PORT) network-test-api:$(VERSION)

fastly-build: ## Build for Fastly (prompts for service_id)
	@if grep -q 'YOUR_SERVICE_ID' fastly.toml; then \
		read -p "Enter your Fastly service_id: " SERVICE_ID; \
		sed -i.bak "s/YOUR_SERVICE_ID/$$SERVICE_ID/" fastly.toml && rm -f fastly.toml.bak; \
	fi
	@echo "$(YELLOW)☁️  Building Fastly WASM...$(RESET)"
	@fastly compute build
	@echo "$(GREEN)✅ Fastly build complete$(RESET)"

fastly-deploy: fastly-build ## Deploy to Fastly
	@echo "$(YELLOW)🚀 Deploying...$(RESET)"
	@fastly compute deploy
	@echo "$(GREEN)✅ Deployed$(RESET)"

fastly-logs: ## Tail Fastly logs
	@fastly log-tail

install: setup ## Alias for setup

all: setup test-bg ## Setup + test

# Test Suite Commands
test-unit: ## Run unit tests
	@echo "$(YELLOW)🧪 Running unit tests...$(RESET)"
	@go test -v -race ./tests/unit/...
	@echo "$(GREEN)✅ Unit tests complete$(RESET)"

test-integration: ## Run integration tests
	@echo "$(YELLOW)🧪 Running integration tests...$(RESET)"
	@go test -v -race ./tests/integration/...
	@echo "$(GREEN)✅ Integration tests complete$(RESET)"

test-functional: ## Run functional tests
	@echo "$(YELLOW)🧪 Running functional tests...$(RESET)"
	@go test -v -race ./tests/functional/...
	@echo "$(GREEN)✅ Functional tests complete$(RESET)"

test-e2e: ## Run E2E tests
	@echo "$(YELLOW)🧪 Running E2E tests...$(RESET)"
	@go test -v -race ./tests/e2e/...
	@echo "$(GREEN)✅ E2E tests complete$(RESET)"

test-acceptance: ## Run acceptance tests
	@echo "$(YELLOW)🧪 Running acceptance tests...$(RESET)"
	@go test -v -race ./tests/acceptance/...
	@echo "$(GREEN)✅ Acceptance tests complete$(RESET)"

test-all: ## Run all test suites
	@echo "$(YELLOW)🧪 Running all tests...$(RESET)"
	@go test -v -race ./tests/...
	@echo "$(GREEN)✅ All tests complete$(RESET)"

test-coverage: ## Run tests with coverage
	@echo "$(YELLOW)🧪 Running tests with coverage...$(RESET)"
	@go test -v -race -coverprofile=coverage.out ./tests/...
	@go tool cover -func=coverage.out
	@go tool cover -html=coverage.out -o coverage.html
	@echo "$(GREEN)✅ Coverage report: coverage.html$(RESET)"

lint: ## Run linter
	@echo "$(YELLOW)🔍 Running linter...$(RESET)"
	@golangci-lint run --timeout=5m
	@echo "$(GREEN)✅ Lint complete$(RESET)"

.DEFAULT_GOAL := help

