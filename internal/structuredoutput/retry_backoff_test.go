package structuredoutput

import (
	"context"
	"testing"
	"time"
)

func TestBackoffWaitCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := backoffWait(ctx, 1); err == nil {
		t.Fatal("expected ctx error when context already canceled")
	}
}

func TestBackoffWaitWaitsAtLeastBase(t *testing.T) {
	start := time.Now()
	if err := backoffWait(context.Background(), 1); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if elapsed := time.Since(start); elapsed < time.Second {
		t.Fatalf("expected >=1s backoff for attempt 1, got %v", elapsed)
	}
}

func TestBackoffWaitCappedAndPositive(t *testing.T) {
	// 大 attempt 不应因移位溢出变成 0/负值导致 panic（rand.Int63n(0) 会 panic）；
	// 应被 cap 到 10s 上限附近。用极短超时确认 ctx 能中断、且不 panic。
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := backoffWait(ctx, 64); err == nil {
		t.Fatal("expected ctx deadline to interrupt a capped long backoff")
	}
}
