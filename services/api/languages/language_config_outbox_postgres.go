package languages

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// insertLanguageConfigOutbox writes the exact event produced by a new active
// configuration through the caller's transaction, so either both facts commit
// or neither does.
func insertLanguageConfigOutbox(ctx context.Context, tx pgx.Tx, config LanguageConfig, traceID string) error {
	event, err := languageConfigChangedEvent(config, traceID)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal language config change event: %w", err)
	}
	payloadHash := sha256.Sum256(payload)
	if _, err := tx.Exec(ctx, `
INSERT INTO language_config_outbox (
    id, language_config_id, event_id, session_id, payload, payload_hash,
    available_at, created_at
) VALUES ($1,$2,$3,$4,$5::jsonb,$6,$7,$7)`,
		"language_config_outbox_"+config.ID,
		config.ID,
		event.EventID,
		config.SessionID,
		payload,
		payloadHash[:],
		config.CreatedAt,
	); err != nil {
		return fmt.Errorf("insert language config outbox: %w", err)
	}
	return nil
}
