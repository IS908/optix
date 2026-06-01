package analysis

import (
	"context"
	"testing"
	"time"
)

func TestClientAppliesTimeoutWhenCallerHasNoDeadline(t *testing.T) {
	c, err := newClientWithRPCTimeout("127.0.0.1:1", 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	done := make(chan error, 1)
	go func() {
		_, _, err := c.GetMaxPain(context.Background(), "AAPL", nil)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected GetMaxPain to fail against an unreachable analysis server")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("GetMaxPain did not return; client RPCs need a deadline when caller context has none")
	}
}

func TestClientPreservesCallerDeadline(t *testing.T) {
	c := &Client{rpcTimeout: 50 * time.Millisecond}
	parentDeadline := time.Now().Add(5 * time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), parentDeadline)
	defer cancel()

	got, gotCancel := c.contextWithRPCDeadline(ctx)
	defer gotCancel()

	deadline, ok := got.Deadline()
	if !ok {
		t.Fatal("expected caller deadline to be preserved")
	}
	if !deadline.Equal(parentDeadline) {
		t.Fatalf("deadline = %s, want %s", deadline, parentDeadline)
	}
}
