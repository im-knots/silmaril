.PHONY: all build clean test test-unit test-integration test-functional coverage lint fmt help

# Variables
BINARY_NAME := silmaril
BUILD_DIR := build
COVERAGE_DIR := .coverage
GO := go
GOFLAGS := -v
LDFLAGS := -s -w

# Default target
all: build

## build: Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/silmaril

## clean: Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)
	@rm -rf $(COVERAGE_DIR)
	@rm -f coverage.txt coverage.html
	@rm -f *.test
	@rm -f *.out

## test: Run all tests
test: test-unit test-functional test-integration

## test-unit: Run unit tests
test-unit:
	@echo "Running unit tests..."
	$(GO) test $(GOFLAGS) -short -race ./...

## test-functional: Run functional tests
test-functional:
	@echo "Running functional tests..."
	$(GO) test $(GOFLAGS) -race ./cmd/silmaril/...

## test-integration: Run integration tests
test-integration:
	@echo "Running integration tests..."
	$(GO) test $(GOFLAGS) -tags=integration -race ./test/integration/...

## coverage: Generate test coverage report
coverage:
	@echo "Generating coverage report..."
	@mkdir -p $(COVERAGE_DIR)
	$(GO) test -coverprofile=$(COVERAGE_DIR)/coverage.out -covermode=atomic ./...
	$(GO) tool cover -html=$(COVERAGE_DIR)/coverage.out -o $(COVERAGE_DIR)/coverage.html
	$(GO) tool cover -func=$(COVERAGE_DIR)/coverage.out
	@echo "Coverage report generated at $(COVERAGE_DIR)/coverage.html"

## coverage-ci: Generate coverage for CI (outputs to coverage.txt)
coverage-ci:
	@echo "Generating CI coverage report..."
	$(GO) test -race -coverprofile=coverage.txt -covermode=atomic ./...

## lint: Run linters
lint:
	@echo "Running linters..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed. Install with:"; \
		echo "  go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
		exit 1; \
	fi

## fmt: Format code
fmt:
	@echo "Formatting code..."
	$(GO) fmt ./...
	@if command -v goimports >/dev/null 2>&1; then \
		goimports -w .; \
	else \
		echo "goimports not installed. Install with:"; \
		echo "  go install golang.org/x/tools/cmd/goimports@latest"; \
	fi

## vet: Run go vet
vet:
	@echo "Running go vet..."
	$(GO) vet ./...

## mod: Download and tidy dependencies
mod:
	@echo "Downloading dependencies..."
	$(GO) mod download
	$(GO) mod tidy

## install: Install the binary
install: build
	@echo "Installing $(BINARY_NAME)..."
	@mkdir -p $(HOME)/.local/bin
	@cp $(BUILD_DIR)/$(BINARY_NAME) $(HOME)/.local/bin/
	@echo "Installed to $(HOME)/.local/bin/$(BINARY_NAME)"

## run: Build and run the binary
run: build
	@echo "Running $(BINARY_NAME)..."
	@$(BUILD_DIR)/$(BINARY_NAME)

## docker-build: Build Docker image
docker-build:
	@echo "Building Docker image..."
	docker build -t $(BINARY_NAME):latest .

## bench: Run benchmarks
bench:
	@echo "Running benchmarks..."
	$(GO) test -bench=. -benchmem ./...

## test-verbose: Run tests with verbose output
test-verbose:
	@echo "Running tests (verbose)..."
	$(GO) test -v -race ./...

## check: Run all checks (fmt, vet, lint, test)
check: fmt vet lint test

## help: Show this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^## ' Makefile | sed 's/## /  /'