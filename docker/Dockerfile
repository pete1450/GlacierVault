# ── Stage 1: Build Go API ───────────────────────────────────────────────────
FROM golang:1.25-trixie AS go-builder

WORKDIR /build/api
COPY api/go.mod ./
# Copy source before tidy so go can resolve all imports.
COPY api/ ./
RUN go mod tidy && go mod download
RUN CGO_ENABLED=1 GOOS=linux go build -o /out/glaciervault-api ./cmd/server

# ── Stage 2: Build Next.js frontend ─────────────────────────────────────────
FROM node:24-trixie-slim AS frontend-builder

WORKDIR /build/frontend
COPY frontend/package*.json ./
RUN npm ci

COPY frontend/ ./
# Point API calls to the Go backend (same container via localhost).
ENV NEXT_TELEMETRY_DISABLED=1
RUN npm run build

# ── Stage 3: Runtime image ───────────────────────────────────────────────────
FROM debian:trixie-slim

# Core runtime dependencies.
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    unzip \
    git \
    nodejs \
    npm \
    python3 \
    python3-pip \
    && rm -rf /var/lib/apt/lists/*

# Apprise: notification dispatcher (Discord, Slack, Telegram, email, ntfy,
# webhooks, ...). The Go API shells out to the `apprise` CLI; destinations
# are user-configured URLs in Settings.
RUN pip3 install --break-system-packages --no-cache-dir apprise \
    && apprise --version

# AWS CLI v2.
RUN curl -fsSL "https://awscli.amazonaws.com/awscli-exe-linux-x86_64.zip" -o /tmp/awscli.zip \
    && unzip -q /tmp/awscli.zip -d /tmp \
    && /tmp/aws/install \
    && rm -rf /tmp/awscli.zip /tmp/aws

# Rustic backup tool.
ARG RUSTIC_VERSION=0.11.4
RUN curl -fsSL "https://github.com/rustic-rs/rustic/releases/download/v${RUSTIC_VERSION}/rustic-v${RUSTIC_VERSION}-x86_64-unknown-linux-gnu.tar.gz" \
    | tar -xz -C /usr/local/bin rustic \
    && chmod +x /usr/local/bin/rustic \
    && rustic --version

# warmup-s3-archives: Glacier restore orchestrator used as rustic's
# --warm-up-command. Released as a standalone binary (not a tarball) from
# the upstream GitLab project philipmw/warmup-s3-archives.
ARG WARMUP_VERSION=1.3.0
RUN curl -fsSL -o /usr/local/bin/warmup-s3-archives \
        "https://gitlab.com/philipmw/warmup-s3-archives/-/releases/v${WARMUP_VERSION}/downloads/warmup-s3-archives-v${WARMUP_VERSION}-linux-x86_64" \
    && chmod +x /usr/local/bin/warmup-s3-archives \
    && test -x /usr/local/bin/warmup-s3-archives

# glaciervault-warmup: wrapper around warmup-s3-archives used as rustic's
# --warm-up-command. The tool's SQS wait budget is expiration_in_days × 24h
# with no separate knob; the wrapper retries it on its wait timeout so the
# total wait covers Bulk's 48h SLA while restored copies keep the 1-day expiry.
COPY docker/glaciervault-warmup.sh /usr/local/bin/glaciervault-warmup
RUN chmod +x /usr/local/bin/glaciervault-warmup \
    && test -x /usr/local/bin/glaciervault-warmup

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
