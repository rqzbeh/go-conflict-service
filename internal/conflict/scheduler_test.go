package conflict

import (
	"context"
	"testing"
	"time"
)

func TestScheduledScanCanStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		StartScheduledScan(ctx, NewStore(), time.Millisecond, nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduled scan did not stop after context cancellation")
	}
}
