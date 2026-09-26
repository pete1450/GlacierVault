#!/bin/bash
# glaciervault-warmup — rustic --warm-up-command wrapper around
# warmup-s3-archives.
#
# Two jobs:
#
# 1. Per-batch copy expiry. rustic warms packs in sequential batches
#    (--warm-up-batch 1000): batch k+1 is only submitted after batch k has
#    fully thawed, and downloads start after the LAST batch thaws. But a
#    restored S3 copy lives only expiration_in_days from its own thaw, so a
#    single expiry for all batches is either too short (early batches expire
#    before the download starts) or wastefully long (late batches linger).
#    GlacierVault therefore writes warmup-plan.txt (batches=N, download_days=DL)
#    next to the tool config, and this wrapper sets each batch's
#    expiration_in_days individually before invoking the tool:
#        E_k = 2*(N-k) + DL + 1
#    2*(N-k) days cover the worst case where every later batch takes the full
#    48h Bulk SLA to thaw; DL covers the download phase on a conservative
#    link (0 for single-batch restores); +1 is the AWS minimum. A single
#    batch keeps exactly 1 day.
#
# 2. SQS wait retries. The upstream tool derives its SQS wait budget as
#    expiration_in_days × 24h and offers no separate timeout knob, so with a
#    1-day expiry it would give up on a healthy BULK thaw that can take up to
#    48h. This wrapper retries the tool when it reports its wait timeout: up
#    to MAX_ATTEMPTS attempts × 24h = 72h total per batch.
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

CONFIG_FILE="warmup-s3-archives-config.toml"
PLAN_FILE="warmup-plan.txt"
COUNTER_FILE="warmup-batch-counter.txt"

# plan_value <key> <default>: read an integer from warmup-plan.txt.
plan_value() {
    local val
    val="$(sed -n "s/^$1=//p" "$PLAN_FILE" 2>/dev/null | head -n 1)"
    case "$val" in
        ''|*[!0-9]*) echo "$2" ;;
        *) echo "$val" ;;
    esac
}

TOTAL_BATCHES=$(plan_value batches 1)
DOWNLOAD_DAYS=$(plan_value download_days 0)
DATA_PACKS=$(plan_value data_packs 0)

# Figure out which batch this invocation is. rustic invokes the wrapper once
# per batch, in order, blocking on each — so a counter file is exact. The
# argument hash guards against double-counting if an invocation ever repeats
# the same key set (retries inside this wrapper do NOT re-enter; they loop
# below without touching the counter).
ARGS_HASH="$(printf '%s\n' "$@" | sha256sum | cut -d' ' -f1)"
BATCH=1
if [ -f "$COUNTER_FILE" ]; then
    prev_batch=$(sed -n 's/^batch=//p' "$COUNTER_FILE" | head -n 1)
    prev_hash=$(sed -n 's/^hash=//p' "$COUNTER_FILE" | head -n 1)
    case "$prev_batch" in
        ''|*[!0-9]*) prev_batch=0 ;;
    esac
    if [ "$prev_hash" = "$ARGS_HASH" ]; then
        BATCH=$prev_batch          # same key set re-invoked: same batch
    else
        BATCH=$((prev_batch + 1))  # new key set: next batch
    fi
fi
[ "$BATCH" -lt 1 ] && BATCH=1
printf 'batch=%s\nhash=%s\n' "$BATCH" "$ARGS_HASH" > "$COUNTER_FILE"

# Per-batch restored-copy expiry: E_k = 2*(N-k) + DL + 1.
EXPIRY_DAYS=$(( 2 * (TOTAL_BATCHES - BATCH) + DOWNLOAD_DAYS + 1 ))
[ "$EXPIRY_DAYS" -lt 1 ] && EXPIRY_DAYS=1

if [ -f "$CONFIG_FILE" ]; then
    sed -i "s/^expiration_in_days = .*/expiration_in_days = $EXPIRY_DAYS/" "$CONFIG_FILE"
fi

echo "glaciervault-warmup: batch $BATCH/$TOTAL_BATCHES ($DATA_PACKS data packs in index) — expiration_in_days=$EXPIRY_DAYS for this batch's restored copies (= 2*($TOTAL_BATCHES-$BATCH) + ${DOWNLOAD_DAYS} download-days + 1); SQS watch budget ~$((EXPIRY_DAYS * 24))h" >&2

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
