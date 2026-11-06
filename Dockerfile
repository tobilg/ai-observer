ARG GOLANG_VERSION=1.26
ARG TARGETARCH=amd64
ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG BUILD_DATE=unknown
ARG SOURCE_DATE_EPOCH=0

# Builder stage
FROM docker.io/library/golang:${GOLANG_VERSION}-bookworm AS builder
LABEL stage=aiobserverbuilder

# Install Node.js, pnpm, and build dependencies for CGO (DuckDB)
RUN apt-get update && apt-get install -y build-essential pkg-config \
    && curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
    && apt-get install -y nodejs \
    && npm install -g pnpm@10.34.7 \
    && rm -rf /var/lib/apt/lists/*

ARG TARGETARCH
ARG VERSION
ARG GIT_COMMIT
ARG BUILD_DATE
ARG SOURCE_DATE_EPOCH

WORKDIR /app

# Dependencies first: these layers are rebuilt only when the manifests change
COPY backend/go.mod backend/go.sum ./backend/
RUN cd backend && go mod download
COPY frontend/package.json frontend/pnpm-lock.yaml frontend/pnpm-workspace.yaml ./frontend/
RUN cd frontend && pnpm install --frozen-lockfile --ignore-scripts && pnpm rebuild esbuild

COPY . ./

# Set build variables - VERSION, GIT_COMMIT, BUILD_DATE override Makefile defaults
ENV GOFLAGS="-trimpath -mod=readonly -buildvcs=false" \
    VERSION=${VERSION} \
    GIT_COMMIT=${GIT_COMMIT} \
    BUILD_DATE=${BUILD_DATE} \
    SOURCE_DATE_EPOCH=${SOURCE_DATE_EPOCH} \
    CI=true

# Build frontend + backend (frontend embeds into backend)
# Makefile will use VERSION, GIT_COMMIT, BUILD_DATE from environment
# CGO_ENABLED=1 is required for DuckDB native bindings
RUN CGO_ENABLED=1 make all

# Create data directory for DuckDB
RUN mkdir -p /app/data

# Runtime stage - distroless for minimal attack surface
FROM --platform=linux/${TARGETARCH} gcr.io/distroless/cc-debian12:nonroot

WORKDIR /app
COPY --from=builder /app/bin/ai-observer /app/ai-observer
COPY --from=builder --chown=nonroot:nonroot /app/data /app/data

# Data directory for DuckDB
VOLUME ["/app/data"]

# Set default database path
ENV AI_OBSERVER_DATABASE_PATH=/app/data/ai-observer.duckdb

# Listen on all interfaces inside the container so published ports are reachable
ENV AI_OBSERVER_HOST=0.0.0.0

EXPOSE 8080 4318

ENTRYPOINT ["/app/ai-observer"]
CMD []
