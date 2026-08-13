-- A language configuration and its change event must commit together. The
-- dispatcher may publish a row more than once after a crash; EventID is the
-- stable downstream idempotency key.
CREATE TABLE IF NOT EXISTS language_config_outbox (
    id                  VARCHAR(64) PRIMARY KEY,
    language_config_id  VARCHAR(26) NOT NULL
        REFERENCES voice_session_language_configs(id) ON DELETE RESTRICT,
    event_id            VARCHAR(128) NOT NULL,
    session_id          TEXT NOT NULL,
    payload             JSONB NOT NULL,
    payload_hash        BYTEA NOT NULL CHECK (octet_length(payload_hash) = 32),
    attempts            INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    available_at        TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    published_at        TIMESTAMPTZ,
    last_error          TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS language_config_outbox_config_unique
    ON language_config_outbox (language_config_id);

CREATE UNIQUE INDEX IF NOT EXISTS language_config_outbox_event_unique
    ON language_config_outbox (event_id);

CREATE INDEX IF NOT EXISTS language_config_outbox_pending
    ON language_config_outbox (available_at, created_at)
    WHERE published_at IS NULL;
