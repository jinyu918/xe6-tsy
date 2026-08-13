package languages

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	realtimev1 "github.com/1024XEngineer/xe6-tsy/packages/contracts/realtime/v1"
)

var (
	// ErrLanguageConfigOutboxUnavailable reports an incomplete durable outbox
	// dependency graph. A runtime must fail visibly rather than accept language
	// configuration changes it can never propagate to realtime.
	ErrLanguageConfigOutboxUnavailable = errors.New("language config outbox is unavailable")
	// ErrLanguageConfigOutboxRecordInvalid identifies a durable row whose
	// canonical payload no longer matches its metadata or recorded hash.
	ErrLanguageConfigOutboxRecordInvalid = errors.New("language config outbox record is invalid")
)

// LanguageConfigOutboxRecord is one immutable language.config.changed fact
// claimed after its creating transaction has committed. The record may be
// published more than once after a worker crash; EventID remains the stable
// downstream idempotency key in every payload copy.
type LanguageConfigOutboxRecord struct {
	ID          string
	EventID     string
	SessionID   string
	Payload     []byte
	PayloadHash [sha256.Size]byte
	Attempts    int
}

// LanguageConfigOutboxRepository owns the database/stream hand-off. Claim
// intentionally does not create a long-lived lease: once the transaction
// commits another process can republish the same immutable event, while only a
// successful broker append is allowed to mark it published.
type LanguageConfigOutboxRepository interface {
	ClaimLanguageConfigOutbox(context.Context, int) ([]LanguageConfigOutboxRecord, error)
	MarkLanguageConfigOutboxPublished(context.Context, string) error
	MarkLanguageConfigOutboxFailed(context.Context, string, string, time.Time) error
}

// canonicalPayload reconstructs the typed event before checking its hash. JSONB
// preserves the document but may reorder object keys when it is read back, so
// hashing its transport bytes would reject a valid committed event.
func (record LanguageConfigOutboxRecord) canonicalPayload() ([]byte, error) {
	if strings.TrimSpace(record.ID) == "" || strings.TrimSpace(record.EventID) == "" || strings.TrimSpace(record.SessionID) == "" || len(record.Payload) == 0 {
		return nil, ErrLanguageConfigOutboxRecordInvalid
	}
	var event realtimev1.LanguageConfigChangedEvent
	if err := json.Unmarshal(record.Payload, &event); err != nil {
		return nil, fmt.Errorf("%w: decode payload: %v", ErrLanguageConfigOutboxRecordInvalid, err)
	}
	if err := event.Validate(); err != nil {
		return nil, fmt.Errorf("%w: validate payload: %v", ErrLanguageConfigOutboxRecordInvalid, err)
	}
	if event.EventID != record.EventID || event.SessionID != record.SessionID {
		return nil, fmt.Errorf("%w: event metadata mismatch", ErrLanguageConfigOutboxRecordInvalid)
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("%w: encode payload: %v", ErrLanguageConfigOutboxRecordInvalid, err)
	}
	if sha256.Sum256(payload) != record.PayloadHash {
		return nil, fmt.Errorf("%w: payload hash mismatch", ErrLanguageConfigOutboxRecordInvalid)
	}
	return payload, nil
}
