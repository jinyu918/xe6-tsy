package languageevents

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestNewValkeyStreamRequiresClient(t *testing.T) {
	if _, err := NewValkeyStream(t.Context(), nil, "", "", ""); err == nil {
		t.Fatal("NewValkeyStream() error = nil, want required client error")
	}
}

func TestNewValkeyStreamUsesDefaultsAndAcceptsExistingGroup(t *testing.T) {
	server, client := newTestValkey(t)
	first, err := NewValkeyStream(t.Context(), client, "", "", "")
	if err != nil {
		t.Fatalf("first NewValkeyStream() error = %v", err)
	}
	second, err := NewValkeyStream(t.Context(), client, "", "", "")
	if err != nil {
		t.Fatalf("second NewValkeyStream() error = %v", err)
	}
	if first.stream != defaultLanguageConfigStream || first.group != defaultLanguageConfigGroup || first.consumer != defaultLanguageConfigWorker {
		t.Fatalf("default config = %#v", first)
	}
	if second.stream != first.stream || second.group != first.group {
		t.Fatalf("second config = %#v", second)
	}
	_ = server
}

func TestValkeyStreamReceivesAcknowledgesAndLeavesNackPending(t *testing.T) {
	server, client := newTestValkey(t)
	queue := newTestStream(t, client, "worker-1")
	if err := queue.Publish(t.Context(), []byte(`{"event_version":1}`)); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	message, err := queue.Receive(t.Context())
	if err != nil {
		t.Fatalf("Receive() error = %v", err)
	}
	if string(message.Payload) != `{"event_version":1}` || message.Receipt == "" {
		t.Fatalf("message = %#v", message)
	}
	if err := queue.Nack(t.Context(), message.Receipt); err != nil {
		t.Fatalf("Nack() error = %v", err)
	}
	if got := pendingCount(t, client, queue.stream, queue.group); got != 1 {
		t.Fatalf("pending after Nack = %d, want 1", got)
	}
	if err := queue.Ack(t.Context(), message.Receipt); err != nil {
		t.Fatalf("Ack() error = %v", err)
	}
	if got := pendingCount(t, client, queue.stream, queue.group); got != 0 {
		t.Fatalf("pending after Ack = %d, want 0", got)
	}
	_ = server
}

func TestValkeyStreamReclaimsIdlePendingEntry(t *testing.T) {
	server, client := newTestValkey(t)
	queue := newTestStream(t, client, "worker-1")
	queue.claimIdle = time.Millisecond
	queue.block = time.Millisecond
	if err := queue.Publish(t.Context(), []byte("payload")); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	first, err := queue.Receive(t.Context())
	if err != nil {
		t.Fatalf("first Receive() error = %v", err)
	}
	if err := queue.Nack(t.Context(), first.Receipt); err != nil {
		t.Fatalf("Nack() error = %v", err)
	}
	server.FastForward(2 * time.Millisecond)
	second, err := queue.Receive(context.Background())
	if err != nil {
		t.Fatalf("reclaim Receive() error = %v", err)
	}
	if second.Receipt != first.Receipt {
		t.Fatalf("reclaimed receipt = %q, want %q", second.Receipt, first.Receipt)
	}
}

func TestValkeyStreamPreservesMalformedReceiptForAck(t *testing.T) {
	_, client := newTestValkey(t)
	queue := newTestStream(t, client, "worker-1")
	entryID, err := client.XAdd(t.Context(), &redis.XAddArgs{Stream: queue.stream, Values: map[string]any{"other": "missing payload"}}).Result()
	if err != nil {
		t.Fatalf("XAdd() error = %v", err)
	}
	message, err := queue.Receive(t.Context())
	if err != nil {
		t.Fatalf("Receive() error = %v", err)
	}
	if message.Receipt != entryID || len(message.Payload) != 0 {
		t.Fatalf("message = %#v", message)
	}
	if err := queue.Ack(t.Context(), message.Receipt); err != nil {
		t.Fatalf("Ack() error = %v", err)
	}
	if got := pendingCount(t, client, queue.stream, queue.group); got != 0 {
		t.Fatalf("pending = %d, want 0", got)
	}
}

func TestValkeyStreamReceiveHonorsCanceledContext(t *testing.T) {
	_, client := newTestValkey(t)
	queue := newTestStream(t, client, "worker-1")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := queue.Receive(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Receive(canceled) error = %v, want context canceled", err)
	}
}

func newTestValkey(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	server, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run() error = %v", err)
	}
	t.Cleanup(server.Close)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return server, client
}

func newTestStream(t *testing.T, client *redis.Client, consumer string) *ValkeyStream {
	t.Helper()
	queue, err := NewValkeyStream(t.Context(), client, "lingow:language:config:changed:test", "language-config-test", consumer)
	if err != nil {
		t.Fatalf("NewValkeyStream() error = %v", err)
	}
	return queue
}

func pendingCount(t *testing.T, client *redis.Client, stream, group string) int64 {
	t.Helper()
	summary, err := client.XPending(t.Context(), stream, group).Result()
	if err != nil {
		t.Fatalf("XPending() error = %v", err)
	}
	return summary.Count
}
