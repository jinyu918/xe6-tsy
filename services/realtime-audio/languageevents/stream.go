package languageevents

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	defaultLanguageConfigStream = "lingow:language:config:changed"
	defaultLanguageConfigGroup  = "lingow-realtime-language-config"
	defaultLanguageConfigWorker = "realtime-language-config-worker"
	defaultStreamBlock          = 5 * time.Second
	defaultClaimIdle            = 30 * time.Second
)

// StreamMessage contains the immutable payload and broker receipt for one
// language.config.changed stream entry. The receipt must be ACKed only after
// the corresponding binding preparation has succeeded.
type StreamMessage struct {
	Payload []byte
	Receipt string
}

// StreamConsumer is the narrow broker contract used by the language event
// processor. Nack intentionally leaves a receipt pending for XAUTOCLAIM.
type StreamConsumer interface {
	Receive(context.Context) (StreamMessage, error)
	Ack(context.Context, string) error
	Nack(context.Context, string) error
}

// ValkeyStream consumes language.config.changed entries from a dedicated
// Redis/Valkey consumer group. It never publishes or creates a group on the
// API side, so producer and consumer ownership remain separate.
type ValkeyStream struct {
	client     redis.UniversalClient
	stream     string
	group      string
	consumer   string
	block      time.Duration
	claimIdle  time.Duration
	claimStart string
}

// NewValkeyStream creates the consumer group if needed. BUSYGROUP is accepted
// so restarts and multiple realtime instances can share the same group.
func NewValkeyStream(ctx context.Context, client redis.UniversalClient, stream, group, consumer string) (*ValkeyStream, error) {
	if nilRedisClient(client) {
		return nil, errors.New("valkey client is required")
	}
	stream = strings.TrimSpace(stream)
	if stream == "" {
		stream = defaultLanguageConfigStream
	}
	group = strings.TrimSpace(group)
	if group == "" {
		group = defaultLanguageConfigGroup
	}
	consumer = strings.TrimSpace(consumer)
	if consumer == "" {
		consumer = defaultLanguageConfigWorker
	}
	queue := &ValkeyStream{
		client: client, stream: stream, group: group, consumer: consumer,
		block: defaultStreamBlock, claimIdle: defaultClaimIdle, claimStart: "0-0",
	}
	if err := client.XGroupCreateMkStream(ctx, stream, group, "0").Err(); err != nil && !isBusyGroup(err) {
		return nil, err
	}
	return queue, nil
}

// Publish appends one payload for offline transport tests and local fixtures.
// Production language events are published by the API outbox worker.
func (q *ValkeyStream) Publish(ctx context.Context, payload []byte) error {
	if q == nil || nilRedisClient(q.client) {
		return errors.New("valkey stream is required")
	}
	return q.client.XAdd(ctx, &redis.XAddArgs{
		Stream: q.stream,
		Values: map[string]any{"payload": payload},
	}).Err()
}

// Receive first reclaims idle pending entries, then waits for new group
// entries. The cursor is retained across calls so a populated pending list is
// scanned in bounded windows rather than repeatedly returning its first item.
func (q *ValkeyStream) Receive(ctx context.Context) (StreamMessage, error) {
	if q == nil || nilRedisClient(q.client) {
		return StreamMessage{}, errors.New("valkey stream is required")
	}
	for {
		if err := ctx.Err(); err != nil {
			return StreamMessage{}, err
		}
		if message, ok, err := q.autoclaim(ctx); err != nil {
			return StreamMessage{}, err
		} else if ok {
			return message, nil
		}
		streams, err := q.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group: q.group, Consumer: q.consumer, Streams: []string{q.stream, ">"},
			Count: 1, Block: q.block,
		}).Result()
		if errors.Is(err, redis.Nil) {
			continue
		}
		if err != nil {
			return StreamMessage{}, err
		}
		if message, ok := streamMessageFromStreams(streams); ok {
			return message, nil
		}
	}
}

func (q *ValkeyStream) Ack(ctx context.Context, receipt string) error {
	if q == nil || nilRedisClient(q.client) {
		return errors.New("valkey stream is required")
	}
	if strings.TrimSpace(receipt) == "" {
		return nil
	}
	return q.client.XAck(ctx, q.stream, q.group, receipt).Err()
}

func (q *ValkeyStream) Nack(_ context.Context, _ string) error {
	// Redis Streams have no negative acknowledgement. Leaving the entry in the
	// pending list makes it eligible for a later XAUTOCLAIM retry.
	return nil
}

func (q *ValkeyStream) autoclaim(ctx context.Context) (StreamMessage, bool, error) {
	start := q.claimStart
	if start == "" {
		start = "0-0"
	}
	entries, next, err := q.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream: q.stream, Group: q.group, Consumer: q.consumer,
		MinIdle: q.claimIdle, Start: start, Count: 1,
	}).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return StreamMessage{}, false, err
	}
	if next == "" {
		next = "0-0"
	}
	q.claimStart = next
	if len(entries) == 0 {
		return StreamMessage{}, false, nil
	}
	return streamMessageFromEntry(entries[0]), true, nil
}

func streamMessageFromStreams(streams []redis.XStream) (StreamMessage, bool) {
	if len(streams) == 0 || len(streams[0].Messages) == 0 {
		return StreamMessage{}, false
	}
	return streamMessageFromEntry(streams[0].Messages[0]), true
}

func streamMessageFromEntry(entry redis.XMessage) StreamMessage {
	return StreamMessage{Payload: bytesField(entry.Values, "payload"), Receipt: entry.ID}
}

func bytesField(values map[string]any, key string) []byte {
	switch value := values[key].(type) {
	case string:
		return []byte(value)
	case []byte:
		return value
	default:
		return nil
	}
}

func nilRedisClient(client redis.UniversalClient) bool {
	switch typed := client.(type) {
	case nil:
		return true
	case *redis.Client:
		return typed == nil
	case *redis.ClusterClient:
		return typed == nil
	default:
		return false
	}
}

func isBusyGroup(err error) bool {
	return err != nil && strings.Contains(err.Error(), "BUSYGROUP")
}

var _ StreamConsumer = (*ValkeyStream)(nil)
