package engine

import (
	"context"
	"testing"
)

func TestCancelRegistry(t *testing.T) {
	// Clean slate.
	UnregisterCancel(999001)

	if HasCancel(999001) {
		t.Fatal("should not have cancel for unknown job")
	}
	if CancelJob(999001) {
		t.Fatal("CancelJob should return false for unknown job")
	}

	ctx, cancel := context.WithCancel(context.Background())
	RegisterCancel(999001, cancel)
	if !HasCancel(999001) {
		t.Fatal("should have cancel after register")
	}
	if !CancelJob(999001) {
		t.Fatal("CancelJob should return true")
	}
	// Cancelled context should be done.
	select {
	case <-ctx.Done():
	default:
		t.Fatal("context should be cancelled")
	}
	// CancelJob does not unregister; explicit UnregisterCancel removes it.
	UnregisterCancel(999001)
	if HasCancel(999001) {
		t.Fatal("should not have cancel after UnregisterCancel")
	}

	// Unregister is idempotent.
	UnregisterCancel(999001)
	UnregisterCancel(999001)
}

func TestRingBuffer(t *testing.T) {
	rb := NewRingBuffer(3)
	rb.Write("a")
	rb.Write("b")
	if lines := rb.Lines(); len(lines) != 2 || lines[0] != "a" {
		t.Fatalf("got %v", lines)
	}
	// Overflow keeps the last N.
	rb.Write("c")
	rb.Write("d")
	lines := rb.Lines()
	if len(lines) != 3 || lines[0] != "b" || lines[2] != "d" {
		t.Fatalf("overflow: got %v", lines)
	}
}

func TestMin(t *testing.T) {
	if min(1, 2) != 1 || min(2, 1) != 1 || min(3, 3) != 3 {
		t.Fatal("min wrong")
	}
}

func TestGetBuffer(t *testing.T) {
	// Unknown job returns nil or a buffer; must not panic.
	_ = GetBuffer(999002)
}
