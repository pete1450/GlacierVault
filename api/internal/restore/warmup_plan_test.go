package restore

import (
	"testing"
	"time"
)

func TestComputeWarmupPlan_SingleBatch(t *testing.T) {
	p := ComputeWarmupPlan(3, 250)
	if p.Batches != 1 {
		t.Fatalf("batches=%d, want 1", p.Batches)
	}
	if p.DownloadDays != 0 {
		t.Fatalf("dl=%d, want 0", p.DownloadDays)
	}
	if got := p.ExpiryDays(1); got != 1 {
		t.Fatalf("expiry batch1=%d, want 1", got)
	}
	if p.Timeout != 96*time.Hour {
		t.Fatalf("timeout=%v, want 96h", p.Timeout)
	}
}

func TestComputeWarmupPlan_BatchBoundary(t *testing.T) {
	// Exactly 1000 packs is still one batch; 1001 needs two.
	if p := ComputeWarmupPlan(1000, 250); p.Batches != 1 {
		t.Fatalf("1000 packs: batches=%d, want 1", p.Batches)
	}
	p := ComputeWarmupPlan(1001, 250)
	if p.Batches != 2 {
		t.Fatalf("1001 packs: batches=%d, want 2", p.Batches)
	}
	// 1001 × 0.5 GB = 500.5 GB → ceil(500.5/250) = 3 download days.
	if p.DownloadDays != 3 {
		t.Fatalf("dl=%d, want 3", p.DownloadDays)
	}
	if got := p.ExpiryDays(1); got != 2*1+3+1 {
		t.Fatalf("expiry batch1=%d, want 6", got)
	}
	if got := p.ExpiryDays(2); got != 0+3+1 {
		t.Fatalf("expiry batch2=%d, want 4", got)
	}
	// Timeout: 2×72h warm-up + 24h×(3+1) download headroom = 240h.
	if p.Timeout != 240*time.Hour {
		t.Fatalf("timeout=%v, want 240h", p.Timeout)
	}
}

func TestComputeWarmupPlan_FullBatches(t *testing.T) {
	// 2000 packs, 1000 GB → DL = 4; matches the documented example table.
	p := ComputeWarmupPlan(2000, 250)
	if p.Batches != 2 || p.DownloadDays != 4 {
		t.Fatalf("got batches=%d dl=%d, want 2/4", p.Batches, p.DownloadDays)
	}
	if got := p.ExpiryDays(1); got != 7 {
		t.Fatalf("expiry batch1=%d, want 7", got)
	}
	if got := p.ExpiryDays(2); got != 5 {
		t.Fatalf("expiry batch2=%d, want 5", got)
	}
}

func TestComputeWarmupPlan_Clamps(t *testing.T) {
	p := ComputeWarmupPlan(0, 250)
	if p.Batches != 1 || p.DataPacks != 1 {
		t.Fatalf("zero packs should still plan one batch, got %+v", p)
	}
	// Degenerate download rate must not divide by zero or go negative.
	p = ComputeWarmupPlan(2000, 0)
	if p.DownloadDays < 1 {
		t.Fatalf("dl=%d, want >= 1", p.DownloadDays)
	}
	// Out-of-range batch numbers floor at the AWS minimum of 1 day.
	p = ComputeWarmupPlan(1, 250)
	if got := p.ExpiryDays(99); got != 1 {
		t.Fatalf("out-of-range batch expiry=%d, want min 1", got)
	}
}
