package languages

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"
)

// ClaimLanguageConfigOutbox reserves the current eligible batch with row locks
// only long enough to increment its attempt count. The short claim transaction
// avoids concurrent duplicate work where possible; duplicate publishes remain
// explicitly safe when a process fails after the transaction commits.
func (s *PostgresStore) ClaimLanguageConfigOutbox(ctx context.Context, limit int) ([]LanguageConfigOutboxRecord, error) {
	if s == nil || s.pool == nil {
		return nil, ErrLanguageConfigOutboxUnavailable
	}
	if limit < 1 {
		limit = 50
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin language config outbox claim: %w", err)
	}
	defer tx.Rollback(ctx)

	rows, err := tx.Query(ctx, `
SELECT id, event_id, session_id, payload, payload_hash, attempts
FROM language_config_outbox
WHERE published_at IS NULL
  AND available_at <= CURRENT_TIMESTAMP
ORDER BY created_at
FOR UPDATE SKIP LOCKED
LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("claim language config outbox: %w", err)
	}
	records := make([]LanguageConfigOutboxRecord, 0, limit)
	for rows.Next() {
		var (
			record  LanguageConfigOutboxRecord
			hashRaw []byte
		)
		if err := rows.Scan(&record.ID, &record.EventID, &record.SessionID, &record.Payload, &hashRaw, &record.Attempts); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan language config outbox: %w", err)
		}
		if len(hashRaw) != sha256.Size {
			rows.Close()
			return nil, fmt.Errorf("%w: payload hash for %q has length %d", ErrLanguageConfigOutboxRecordInvalid, record.ID, len(hashRaw))
		}
		copy(record.PayloadHash[:], hashRaw)
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate language config outbox: %w", err)
	}
	rows.Close()

	for index := range records {
		if _, err := tx.Exec(ctx, `
UPDATE language_config_outbox
SET attempts = attempts + 1
WHERE id = $1
  AND published_at IS NULL`, records[index].ID); err != nil {
			return nil, fmt.Errorf("record language config outbox attempt: %w", err)
		}
		records[index].Attempts++
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit language config outbox claim: %w", err)
	}
	return records, nil
}

// MarkLanguageConfigOutboxPublished records only a broker-accepted event. The
// update is idempotent so a second dispatcher can safely settle a duplicate
// publication after the first one has already committed the durable outcome.
func (s *PostgresStore) MarkLanguageConfigOutboxPublished(ctx context.Context, id string) error {
	if s == nil || s.pool == nil {
		return ErrLanguageConfigOutboxUnavailable
	}
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("%w: record id is required", ErrLanguageConfigOutboxRecordInvalid)
	}
	if _, err := s.pool.Exec(ctx, `
UPDATE language_config_outbox
SET published_at = COALESCE(published_at, CURRENT_TIMESTAMP),
    last_error = NULL
WHERE id = $1`, id); err != nil {
		return fmt.Errorf("mark language config outbox published: %w", err)
	}
	return nil
}

// MarkLanguageConfigOutboxFailed preserves an unpublished row for retry. A
// concurrent successful publisher wins by predicate: it cannot be moved back
// to pending after published_at has been set.
func (s *PostgresStore) MarkLanguageConfigOutboxFailed(ctx context.Context, id, reason string, availableAt time.Time) error {
	if s == nil || s.pool == nil {
		return ErrLanguageConfigOutboxUnavailable
	}
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("%w: record id is required", ErrLanguageConfigOutboxRecordInvalid)
	}
	if availableAt.IsZero() {
		availableAt = s.clock.Now().UTC()
	}
	if _, err := s.pool.Exec(ctx, `
UPDATE language_config_outbox
SET last_error = $2,
    available_at = $3
WHERE id = $1
  AND published_at IS NULL`, id, strings.TrimSpace(reason), availableAt.UTC()); err != nil {
		return fmt.Errorf("mark language config outbox failed: %w", err)
	}
	return nil
}

var _ LanguageConfigOutboxRepository = (*PostgresStore)(nil)
