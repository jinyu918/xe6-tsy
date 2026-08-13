package languages

import (
	"context"
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9"
)

const defaultLanguageConfigChangedStream = "lingow:language:config:changed"

// languageConfigStreamClient is the producer-only Valkey surface. Keeping it
// narrow prevents API startup from accidentally creating a consumer group for
// a stream that belongs to realtime-audio consumers.
type languageConfigStreamClient interface {
	XAdd(context.Context, *redis.XAddArgs) *redis.StringCmd
}

// ValkeyLanguageConfigChangedPublisher appends immutable payloads to the
// language.config.changed stream. Stream entries intentionally contain only
// the canonical payload; event_id stays inside that typed payload.
type ValkeyLanguageConfigChangedPublisher struct {
	client languageConfigStreamClient
	stream string
}

// NewValkeyLanguageConfigChangedPublisher selects the dedicated stream and
// performs no network I/O. Runtime startup owns connectivity validation.
func NewValkeyLanguageConfigChangedPublisher(client languageConfigStreamClient, stream string) *ValkeyLanguageConfigChangedPublisher {
	if strings.TrimSpace(stream) == "" {
		stream = defaultLanguageConfigChangedStream
	}
	return &ValkeyLanguageConfigChangedPublisher{client: client, stream: stream}
}

// PublishLanguageConfigChanged durably appends one payload. It does not
// deduplicate stream entries: a process failure between XADD and the Postgres
// acknowledgement is expected and downstream consumers deduplicate event_id.
func (p *ValkeyLanguageConfigChangedPublisher) PublishLanguageConfigChanged(ctx context.Context, payload []byte) error {
	if p == nil || p.client == nil {
		return ErrLanguageConfigOutboxUnavailable
	}
	if len(payload) == 0 {
		return fmt.Errorf("%w: payload is required", ErrLanguageConfigOutboxRecordInvalid)
	}
	if _, err := p.client.XAdd(ctx, &redis.XAddArgs{
		Stream: p.stream,
		Values: map[string]any{"payload": payload},
	}).Result(); err != nil {
		return fmt.Errorf("append language config changed event: %w", err)
	}
	return nil
}

var _ LanguageConfigChangedPublisher = (*ValkeyLanguageConfigChangedPublisher)(nil)
