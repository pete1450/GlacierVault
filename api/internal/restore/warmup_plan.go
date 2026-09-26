package restore

import (
	"math"
	"time"
)

// Warm-up sizing constants. rustic invokes the warm-up command with
// --warm-up-batch packs at a time and blocks on each batch, so batches run
// sequentially: batch k+1's S3 Batch restore job is only submitted after
// batch k has fully thawed.
const (
	// warmupBatchSize must match the --warm-up-batch flag in
	// engine.RunRestore.
	warmupBatchSize = 1000
	// maxPackGB is the configured maximum data pack size (512 MiB); the
	// total-GB estimate is an upper bound since packs are rarely full.
	maxPackGB = 0.5
	// bulkThawMaxHours is the Deep Archive Bulk tier SLA per batch.
	bulkThawMaxHours = 48
	// warmupAttemptHours is the SQS wait budget per wrapper attempt: the
	// tool watches expiration_in_days × 24h, and the wrapper retries up to
	// maxWarmupAttempts times.
	warmupAttemptHours  = 24
	maxWarmupAttempts   = 3
	perBatchWaitHours   = warmupAttemptHours * maxWarmupAttempts // 72
	defaultDownloadRate = 1080                                   // GB/day ≈ 100 Mbit/s
)

// WarmupPlan sizes a restore's Glacier warm-up: how many sequential batches
// rustic will need, how long each batch's restored copies must live, and
// the overall job timeout.
//
// DataPacks is the number of distinct data packs in the repository index —
// an upper bound on what this restore needs (a restore only touches its
// snapshot's packs). Sizing from the upper bound is the safe direction:
// overestimating costs a few extra days of S3 Standard on the packs actually
// thawed (pennies), while underestimating can break a multi-day restore.
type WarmupPlan struct {
	DataPacks int
	Batches   int
	// TotalGB is the upper-bound restore size: DataPacks × 512 MiB.
	TotalGB float64
	// DownloadDays (DL) is the headroom for the download phase after the
	// last thaw, from a conservative download rate. Zero for single-batch
	// restores, which stay at the 1-day copy expiry.
	DownloadDays int
	// Timeout covers the worst-case warm-up (72h per batch: 3 wrapper
	// attempts × 24h SQS budget) plus the download headroom.
	Timeout time.Duration
}

// ComputeWarmupPlan derives the plan from the index pack count and the
// configured download rate (GB/day, clamped to ≥1).
func ComputeWarmupPlan(dataPacks, downloadGBPerDay int) WarmupPlan {
	if dataPacks < 1 {
		dataPacks = 1
	}
	if downloadGBPerDay < 1 {
		downloadGBPerDay = 1
	}
	batches := (dataPacks + warmupBatchSize - 1) / warmupBatchSize
	if batches < 1 {
		batches = 1
	}
	totalGB := float64(dataPacks) * maxPackGB
	dl := 0
	if batches > 1 {
		dl = int(math.Ceil(totalGB / float64(downloadGBPerDay)))
	}
	return WarmupPlan{
		DataPacks:    dataPacks,
		Batches:      batches,
		TotalGB:      totalGB,
		DownloadDays: dl,
		Timeout:      time.Duration(perBatchWaitHours*batches+24*(dl+1)) * time.Hour,
	}
}

// ExpiryDays returns how long batch k's (1-indexed) restored copies must
// live, in whole days (the AWS minimum is 1):
//
//	E_k = 2·(N−k) + DL + 1
//
// 2·(N−k) days cover the worst case where every later batch takes the full
// 48h Bulk SLA to thaw (batch k+1 is only submitted after batch k thaws, so
// the gap telescopes); DL covers the download phase on a conservative link;
// +1 is the AWS minimum. For a single batch this is exactly 1 day.
func (p WarmupPlan) ExpiryDays(batch int) int {
	e := 2*(p.Batches-batch) + p.DownloadDays + 1
	if e < 1 {
		e = 1
	}
	return e
}
