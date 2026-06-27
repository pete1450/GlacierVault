# ── Stage 1: Build Go API ───────────────────────────────────────────────────
FROM golang:1.22-bookworm AS go-builder

WORKDIR /build/api
COPY api/go.mod ./
# Copy source before tidy so go can resolve all imports.
COPY api/ ./
RUN go mod tidy && go mod download
RUN CGO_ENABLED=1 GOOS=linux go build -o /out/glaciervault-api ./cmd/server

# ── Stage 2: Build Next.js frontend ─────────────────────────────────────────
FROM node:20-bookworm-slim AS frontend-builder

WORKDIR /build/frontend
COPY frontend/package*.json ./
RUN npm ci

COPY frontend/ ./
# Point API calls to the Go backend (same container via localhost).
ENV NEXT_TELEMETRY_DISABLED=1
RUN npm run build

# ── Stage 3: Runtime image ───────────────────────────────────────────────────
FROM debian:bookworm-slim

# Core runtime dependencies.
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    unzip \
    git \
    nodejs \
    npm \
    python3 \
    && rm -rf /var/lib/apt/lists/*

# AWS CLI v2.
RUN curl -fsSL "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o /tmp/awscli.zip \
    && unzip -q /tmp/awscli.zip -d /tmp \
    && /tmp/aws/install \
    && rm -rf /tmp/awscli.zip /tmp/aws

# Rustic backup tool.
ARG RUSTIC_VERSION=0.9.4
RUN curl -fsSL "https://github.com/rustic-rs/rustic/releases/download/v${RUSTIC_VERSION}/rustic-v${RUSTIC_VERSION}-x86_64-unknown-linux-gnu.tar.gz" \
    | tar -xz -C /usr/local/bin rustic \
    && chmod +x /usr/local/bin/rustic

# warmup-s3-archives (from rustic-aws).
ARG WARMUP_VERSION=0.3.0
RUN curl -fsSL "https://github.com/rustic-rs/rustic-aws/releases/download/warmup-s3-archives-v${WARMUP_VERSION}/warmup-s3-archives-v${WARMUP_VERSION}-x86_64-unknown-linux-gnu.tar.gz" \
    | tar -xz -C /usr/local/bin warmup-s3-archives \
    && chmod +x /usr/local/bin/warmup-s3-archives || true

# AWS CDK (for infrastructure deployment).
RUN npm install -g aws-cdk@latest --quiet

# Fetch glacier-cold-storage-cdk from rustic-aws (sparse clone — build-time only, not runtime download).
ARG CDK_REF=main
RUN git clone --depth=1 --filter=blob:none --sparse \
        https://github.com/rustic-rs/rustic-aws.git /tmp/rustic-aws \
    && git -C /tmp/rustic-aws sparse-checkout set glacier-cold-storage-cdk \
    && mv /tmp/rustic-aws/glacier-cold-storage-cdk /opt/cdk \
    && rm -rf /tmp/rustic-aws
RUN cd /opt/cdk && npm ci --quiet && npm run build

# Copy built artifacts.
COPY --from=go-builder /out/glaciervault-api /usr/local/bin/glaciervault-api
COPY --from=frontend-builder /build/frontend/out /app/frontend

# Volumes.
VOLUME ["/config", "/database", "/cache", "/logs"]

# Default environment.
ENV CONFIG_DIR=/config \
    DATABASE_PATH=/database/glaciervault.db \
    LISTEN_ADDR=:8080

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/glaciervault-api"]
