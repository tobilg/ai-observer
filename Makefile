.PHONY: all backend frontend backend-dev backend-test backend-lint backend-coverage frontend-dev frontend-test frontend-lint frontend-coverage dev clean test lint coverage setup help release-notes pricing-update check-pricing

# Version information
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

GOFLAGS ?= -trimpath -mod=readonly -buildvcs=false
LDFLAGS ?= -s -w -buildid= \
	-X 'github.com/tobilg/ai-observer/internal/version.Version=$(VERSION)' \
	-X 'github.com/tobilg/ai-observer/internal/version.GitCommit=$(GIT_COMMIT)' \
	-X 'github.com/tobilg/ai-observer/internal/version.BuildDate=$(BUILD_DATE)'
SOURCE_DATE_EPOCH ?= $(shell git log -1 --format=%ct 2>/dev/null || date +%s)

# Binary extension (.exe on Windows)
ifeq ($(GOOS),windows)
	BINARY_EXT := .exe
else
	BINARY_EXT :=
endif
BINARY_NAME := ai-observer$(BINARY_EXT)

# Default target (backend depends on frontend, so just build backend)
all: backend

# Backend targets (depends on frontend for embedding)
backend: frontend
	@echo "Building backend..."
	cd backend && \
	SOURCE_DATE_EPOCH=$(SOURCE_DATE_EPOCH) GOFLAGS="$(GOFLAGS)" \
	go build -ldflags "$(LDFLAGS)" -o ../bin/$(BINARY_NAME) ./cmd/server

backend-dev:
	@echo "Starting backend in development mode..."
	cd backend && AI_OBSERVER_DATABASE_PATH=$(CURDIR)/data/ai-observer.duckdb go run ./cmd/server

backend-dev-watch:
	@echo "Starting backend in development mode..."
	cd backend && AI_OBSERVER_DATABASE_PATH=$(CURDIR)/data/ai-observer.duckdb go run ./cmd/server watch

backend-test:
	@echo "Running backend tests..."
	cd backend && go test -v ./...

# golangci-lint version, keep in sync with the "Lint backend" step in .github/workflows/ci.yml.
# Built with the local Go toolchain, so it can analyze code for the go.mod Go version.
GOLANGCI_LINT_VERSION ?= v2.5.0

backend-lint:
	@echo "Linting backend..."
	cd backend && go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run

# Frontend targets
frontend:
	@echo "Building frontend..."
	cd frontend && pnpm build

frontend-dev:
	@echo "Starting frontend in development mode..."
	cd frontend && pnpm dev

frontend-test:
	@echo "Running frontend tests..."
	cd frontend && pnpm test

frontend-lint:
	@echo "Linting frontend..."
	cd frontend && pnpm lint

# Combined targets
dev:
	@echo "Starting development servers..."
	@make -j2 backend-dev frontend-dev

test: backend-test frontend-test

lint: backend-lint frontend-lint

# Coverage targets
# The toolchain's own go comes first in PATH: when go switches to the
# toolchain go.mod asks for, -cover builds the covdata tool with the go found
# in PATH, and an older local install fails ("compile: version ... does not
# match go tool version ...")
backend-coverage:
	@echo "Generating backend coverage report..."
	cd backend && PATH="$$(go env GOROOT)/bin:$$PATH" go test -coverprofile=coverage.out ./...
	cd backend && go tool cover -func=coverage.out
	cd backend && go tool cover -html=coverage.out -o coverage.html
	@echo "Backend coverage report: backend/coverage.html"

frontend-coverage:
	@echo "Generating frontend coverage report..."
	cd frontend && pnpm test:coverage

coverage: backend-coverage frontend-coverage

# Cleanup
clean:
	@echo "Cleaning build artifacts..."
	rm -rf bin/
	rm -rf frontend/dist/
	rm -rf backend/internal/frontend/dist/

# Setup
setup:
	@echo "Setting up project..."
	cd backend && go mod download
	cd frontend && pnpm install --ignore-scripts
	cd frontend && pnpm rebuild esbuild

# Pricing data (docs/pricing.md)
# Refresh the built-in prices from models.dev (review the diff)
pricing-update: frontend
	cd backend && go run ./cmd/server pricing update --output internal/pricing/data

# Fail when a built-in pricing file is more than 7 days old (also run daily in CI)
check-pricing:
	@./scripts/check-pricing-age.sh 7

# Release notes generation
release-notes:
	@./scripts/generate-release-notes.sh $(TAG)

# Help
help:
	@echo "Available targets:"
	@echo "  all               - Build both backend and frontend"
	@echo "  backend           - Build backend binary"
	@echo "  backend-dev       - Run backend in development mode"
	@echo "  backend-coverage  - Generate backend coverage report"
	@echo "  frontend          - Build frontend for production"
	@echo "  frontend-dev      - Run frontend development server"
	@echo "  frontend-coverage - Generate frontend coverage report"
	@echo "  dev               - Run both backend and frontend in dev mode"
	@echo "  test              - Run all tests"
	@echo "  coverage          - Generate all coverage reports"
	@echo "  lint              - Run linters"
	@echo "  clean             - Remove build artifacts"
	@echo "  setup             - Install dependencies"
	@echo "  release-notes     - Generate release notes with Claude (TAG=vX.X.X)"
	@echo "  pricing-update    - Refresh the built-in prices from models.dev"
	@echo "  check-pricing     - Fail when pricing data is more than 7 days old"
	@echo "  help              - Show this help message"
