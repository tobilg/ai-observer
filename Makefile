.PHONY: all backend frontend backend-dev backend-test backend-lint backend-coverage frontend-dev frontend-test frontend-lint frontend-coverage dev clean test lint coverage setup help release-notes

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
	go clean -cache && \
	SOURCE_DATE_EPOCH=$(SOURCE_DATE_EPOCH) GOFLAGS="$(GOFLAGS)" \
	go build -ldflags "$(LDFLAGS)" -o ../bin/$(BINARY_NAME) ./cmd/server

backend-dev:
	@echo "Starting backend in development mode..."
	cd backend && AI_OBSERVER_DATABASE_PATH=$(CURDIR)/data/ai-observer.duckdb go run ./cmd/server

backend-test:
	@echo "Running backend tests..."
	cd backend && go test -v ./...

backend-lint:
	@echo "Linting backend..."
	cd backend && golangci-lint run

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
backend-coverage:
	@echo "Generating backend coverage report..."
	cd backend && go test -coverprofile=coverage.out ./...
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
	cd frontend && pnpm install

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
	@echo "  help              - Show this help message"
