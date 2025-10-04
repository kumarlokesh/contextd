# contextd Makefile
# Provides convenient commands for development, testing, and deployment

# Variables
CARGO := cargo
DOCKER := docker
COMPOSE := docker-compose
PROJECT_NAME := contextd
VERSION := $(shell grep '^version' Cargo.toml | sed 's/version = "\(.*\)"/\1/')

# Colors for output
GREEN := \033[0;32m
BLUE := \033[0;34m
YELLOW := \033[0;33m
RED := \033[0;31m
NC := \033[0m # No Color

.PHONY: help
help: ## Show this help message
	@echo "$(BLUE)contextd development commands:$(NC)\n"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "$(GREEN)%-20s$(NC) %s\n", $$1, $$2}'

# Development commands
.PHONY: dev
dev: ## Start development server with hot reload
	@echo "$(BLUE)Starting contextd development server...$(NC)"
	$(CARGO) watch -x 'run -- serve'

.PHONY: build
build: ## Build the project in debug mode
	@echo "$(BLUE)Building contextd...$(NC)"
	$(CARGO) build

.PHONY: build-release
build-release: ## Build the project in release mode
	@echo "$(BLUE)Building contextd (release)...$(NC)"
	$(CARGO) build --release

.PHONY: install
install: build-release ## Install contextd and contextctl to system
	@echo "$(BLUE)Installing contextd and contextctl...$(NC)"
	$(CARGO) install --path .

# Testing commands
.PHONY: test
test: ## Run all tests
	@echo "$(BLUE)Running all tests...$(NC)"
	$(CARGO) test --all-features

.PHONY: test-unit
test-unit: ## Run unit tests only
	@echo "$(BLUE)Running unit tests...$(NC)"
	$(CARGO) test --lib --all-features

.PHONY: test-integration
test-integration: ## Run integration tests only
	@echo "$(BLUE)Running integration tests...$(NC)"
	$(CARGO) test --test '*' --all-features

.PHONY: test-coverage
test-coverage: ## Generate test coverage report (requires cargo-tarpaulin)
	@echo "$(BLUE)Generating test coverage report...$(NC)"
	$(CARGO) tarpaulin --all-features --workspace --timeout 120 --out Html --output-dir target/coverage

.PHONY: benchmark
benchmark: ## Run performance benchmarks
	@echo "$(BLUE)Running benchmarks...$(NC)"
	$(CARGO) bench

# Code quality commands
.PHONY: fmt
fmt: ## Format code with rustfmt
	@echo "$(BLUE)Formatting code...$(NC)"
	$(CARGO) fmt --all

.PHONY: fmt-check
fmt-check: ## Check code formatting
	@echo "$(BLUE)Checking code formatting...$(NC)"
	$(CARGO) fmt --all -- --check

.PHONY: clippy
clippy: ## Run clippy linter
	@echo "$(BLUE)Running clippy...$(NC)"
	$(CARGO) clippy --all-features --all-targets -- -D warnings

.PHONY: lint
lint: fmt-check clippy ## Run all linting checks

.PHONY: audit
audit: ## Run security audit
	@echo "$(BLUE)Running security audit...$(NC)"
	$(CARGO) audit

# Documentation commands
.PHONY: doc
doc: ## Generate documentation
	@echo "$(BLUE)Generating documentation...$(NC)"
	$(CARGO) doc --all-features --no-deps --open

.PHONY: doc-deps
doc-deps: ## Generate documentation including dependencies
	@echo "$(BLUE)Generating documentation with dependencies...$(NC)"
	$(CARGO) doc --all-features --open

# Example commands
.PHONY: examples
examples: build ## Build all examples
	@echo "$(BLUE)Building examples...$(NC)"
	cd examples && $(CARGO) build --all-targets

.PHONY: run-basic-example
run-basic-example: ## Run basic integration example
	@echo "$(BLUE)Running basic integration example...$(NC)"
	@echo "$(YELLOW)Make sure contextd is running: make serve$(NC)"
	cd examples && $(CARGO) run --bin basic_integration

.PHONY: run-llm-example
run-llm-example: ## Run LLM memory demo example
	@echo "$(BLUE)Running LLM memory demo...$(NC)"
	@echo "$(YELLOW)Make sure contextd is running: make serve$(NC)"
	cd examples && $(CARGO) run --bin llm_memory_demo

.PHONY: run-batch-example
run-batch-example: ## Run batch operations example
	@echo "$(BLUE)Running batch operations example...$(NC)"
	@echo "$(YELLOW)Make sure contextd is running: make serve$(NC)"
	cd examples && $(CARGO) run --bin batch_operations

.PHONY: demo
demo: ## Run all examples in sequence
	@echo "$(BLUE)Running all examples...$(NC)"
	@echo "$(YELLOW)Starting contextd server in background...$(NC)"
	$(CARGO) run -- serve &
	@server_pid=$$!; \
	sleep 3; \
	echo "$(BLUE)Running basic integration example...$(NC)"; \
	cd examples && $(CARGO) run --bin basic_integration; \
	echo "$(BLUE)Running LLM memory demo...$(NC)"; \
	cd examples && $(CARGO) run --bin llm_memory_demo; \
	echo "$(BLUE)Running batch operations example...$(NC)"; \
	cd examples && $(CARGO) run --bin batch_operations; \
	echo "$(GREEN)All examples completed!$(NC)"; \
	kill $$server_pid

# Server commands
.PHONY: serve
serve: build ## Start contextd server
	@echo "$(BLUE)Starting contextd server...$(NC)"
	$(CARGO) run -- serve

.PHONY: serve-release
serve-release: build-release ## Start contextd server (release build)
	@echo "$(BLUE)Starting contextd server (release)...$(NC)"
	./target/release/contextd serve

# CLI commands
.PHONY: cli-help
cli-help: build ## Show contextctl help
	$(CARGO) run --bin contextctl -- --help

.PHONY: health
health: ## Check server health
	$(CARGO) run --bin contextctl -- health

.PHONY: stats
stats: ## Show server statistics
	$(CARGO) run --bin contextctl -- stats

# Database commands
.PHONY: clean-data
clean-data: ## Clean all data (database, index, logs)
	@echo "$(YELLOW)Removing all data...$(NC)"
	rm -rf data/
	@echo "$(GREEN)Data cleaned!$(NC)"

.PHONY: backup-data
backup-data: ## Backup data directory
	@echo "$(BLUE)Backing up data...$(NC)"
	tar -czf data-backup-$(shell date +%Y%m%d-%H%M%S).tar.gz data/
	@echo "$(GREEN)Data backed up!$(NC)"

# Docker commands
.PHONY: docker-build
docker-build: ## Build Docker image
	@echo "$(BLUE)Building Docker image...$(NC)"
	$(DOCKER) build -t $(PROJECT_NAME):$(VERSION) -t $(PROJECT_NAME):latest .

.PHONY: docker-run
docker-run: docker-build ## Run contextd in Docker
	@echo "$(BLUE)Running contextd in Docker...$(NC)"
	$(DOCKER) run -p 8080:8080 -v contextd_data:/data $(PROJECT_NAME):latest

.PHONY: docker-compose-up
docker-compose-up: ## Start services with docker-compose
	@echo "$(BLUE)Starting services with docker-compose...$(NC)"
	$(COMPOSE) up -d

.PHONY: docker-compose-down
docker-compose-down: ## Stop services with docker-compose
	@echo "$(BLUE)Stopping services with docker-compose...$(NC)"
	$(COMPOSE) down

.PHONY: docker-compose-logs
docker-compose-logs: ## Show docker-compose logs
	$(COMPOSE) logs -f

# Release commands
.PHONY: check-release
check-release: lint test audit ## Run all checks before release
	@echo "$(GREEN)All release checks passed!$(NC)"

.PHONY: tag-release
tag-release: ## Tag a new release (requires VERSION env var)
	@if [ -z "$(VERSION)" ]; then echo "$(RED)VERSION is required$(NC)"; exit 1; fi
	@echo "$(BLUE)Tagging release v$(VERSION)...$(NC)"
	git tag -a v$(VERSION) -m "Release v$(VERSION)"
	git push origin v$(VERSION)

# Utility commands
.PHONY: deps
deps: ## Install development dependencies
	@echo "$(BLUE)Installing development dependencies...$(NC)"
	$(CARGO) install cargo-watch cargo-tarpaulin cargo-audit
	@echo "$(GREEN)Dependencies installed!$(NC)"

.PHONY: update
update: ## Update dependencies
	@echo "$(BLUE)Updating dependencies...$(NC)"
	$(CARGO) update

.PHONY: clean
clean: ## Clean build artifacts
	@echo "$(BLUE)Cleaning build artifacts...$(NC)"
	$(CARGO) clean

.PHONY: reset
reset: clean clean-data ## Reset everything (build artifacts and data)
	@echo "$(GREEN)Everything cleaned!$(NC)"

# Performance commands
.PHONY: profile-cpu
profile-cpu: ## Profile CPU usage (requires perf)
	@echo "$(BLUE)Profiling CPU usage...$(NC)"
	perf record --call-graph=dwarf $(CARGO) run --release -- serve &
	@echo "$(YELLOW)Server started. Run your workload, then press Ctrl+C$(NC)"
	@read -p "Press enter when done..."
	perf report

.PHONY: flamegraph
flamegraph: ## Generate flamegraph (requires cargo-flamegraph)
	@echo "$(BLUE)Generating flamegraph...$(NC)"
	$(CARGO) flamegraph --bin contextd -- serve

# CI/CD simulation
.PHONY: ci
ci: fmt-check clippy test audit ## Simulate CI pipeline
	@echo "$(GREEN)CI pipeline completed successfully!$(NC)"

# Default target
.DEFAULT_GOAL := help
