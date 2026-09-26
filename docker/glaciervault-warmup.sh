#!/bin/bash
# glaciervault-warmup — rustic --warm-up-command wrapper around
# warmup-s3-archives.
#
# The upstream tool derives its SQS wait budget as expiration_in_days × 24h
# and offers no separate timeout knob. GlacierVault keeps restored copies for
# 1 day (the AWS minimum), which would give up on a healthy BULK thaw that
# can take up to 48h. So this wrapper retries the tool when it reports its
# wait timeout: up to MAX_ATTEMPTS attempts × 24h = 72h total, matching the
# restoreTimeout.
#
# Retries are safe:
#   - the tool's RestoreStatus pre-check (ListObjectsV2) short-circuits on
#     packs that thawed between attempts — no SQS wait needed for those;
#   - duplicate S3 Batch restore requests are idempotent.
# Only the tool's wait timeout is retried; any other failure (bad config,
# Batch job submission error, ...) aborts immediately with the tool's output.
#
# Output streams through untouched (merged, as rustic merges them anyway), so
# GlacierVault's Batch-job-ID capture keeps working; a copy is kept only to
# detect the timeout message.
#
# Couplings: timeout detection greps for the tool's log message
# "Timed out waiting for N remaining objects" (warmup-s3-archives 1.3.0).
# The tool reads warmup-s3-archives-config.toml from the working directory;
# this wrapper does not change directories.

MAX_ATTEMPTS=${GLACIERVAULT_WARMUP_MAX_ATTEMPTS:-3}
RETRY_DELAY_SECS=${GLACIERVAULT_WARMUP_RETRY_DELAY_SECS:-60}

tmp="$(mktemp)" || exit 1
attempt=1
while [ "$attempt" -le "$MAX_ATTEMPTS" ]; do
    warmup-s3-archives "$@" 2>&1 | tee "$tmp"
    status=${PIPESTATUS[0]}
    if [ "$status" -eq 0 ]; then
        rm -f "$tmp"
        exit 0
    fi
    if grep -q "Timed out waiting for" "$tmp"; then
        echo "glaciervault-warmup: attempt $attempt/$MAX_ATTEMPTS timed out waiting for Glacier (Bulk can take up to 48h); re-checking restore status..." >&2
        attempt=$((attempt + 1))
        sleep "$RETRY_DELAY_SECS"
        continue
    fi
    # A real error — the tool's output already streamed above; stop.
    rm -f "$tmp"
    exit "$status"
done
echo "glaciervault-warmup: still waiting on Glacier after $MAX_ATTEMPTS attempts; giving up" >&2
rm -f "$tmp"
exit 1
