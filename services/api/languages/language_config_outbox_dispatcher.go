package languages

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

const (
	defaultLanguageConfigOutboxInterval = time.Second
	maxLanguageConfigOutboxRetryDelay   = time.Minute
)

// LanguageConfigChangedPublisher appends one canonical event payload to the
// dedicated language configuration stream. It deliberately has no consume or
// acknowledgement methods because API owns only the producer side of this
// control-plane boundary.
type LanguageConfigChangedPublisher interface {
	PublishLanguageConfigChanged(context.Context, []byte) error
}

// LanguageConfigOutboxDispatcher retries committed configuration changes until
// Valkey accepts their immutable payload. It must keep running after transient
// database or broker failures, because returning would strand an active config
// whose realtime runtime has not yet observed the required binding change.
type LanguageConfigOutboxDispatcher struct {
	repository LanguageConfigOutboxRepository
	publisher  LanguageConfigChangedPublisher
	interval   time.Duration
	now        func() time.Time
	logger     *slog.Logger
}

// NewLanguageConfigOutboxDispatcher constructs a restart-safe publisher loop.
// A nil logger is allowed for focused unit tests; production wiring supplies
// the process logger so transient delivery failures remain observable.
func NewLanguageConfigOutboxDispatcher(
	repository LanguageConfigOutboxRepository,
	publisher LanguageConfigChangedPublisher,
	interval time.Duration,
	logger *slog.Logger,
) *LanguageConfigOutboxDispatcher {
	if interval <= 0 {
		interval = defaultLanguageConfigOutboxInterval
	}
	return &LanguageConfigOutboxDispatcher{
		repository: repository,
		publisher:  publisher,
		interval:   interval,
		now:        func() time.Time { return time.Now().UTC() },
		logger:     logger,
	}
}

// Run performs an eager dispatch then continues polling until the caller
// cancels its context. Each record has independent retry state, so one failed
// broker append must not block newer committed language configurations.
func (d *LanguageConfigOutboxDispatcher) Run(ctx context.Context) error {
	if err := d.validate(); err != nil {
		return err
	}
	d.dispatch(ctx)
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			d.dispatch(ctx)
		}
	}
}

// DispatchOnce publishes every currently eligible row. A publication failure
// is recorded with a bounded retry delay and does not abort later rows; an
// inability to persist that failure is returned because its retry outcome is
// then unknown.
func (d *LanguageConfigOutboxDispatcher) DispatchOnce(ctx context.Context) error {
	if err := d.validate(); err != nil {
		return err
	}
	records, err := d.repository.ClaimLanguageConfigOutbox(ctx, 50)
	if err != nil {
		return fmt.Errorf("claim language config outbox: %w", err)
	}
	for _, record := range records {
		payload, err := record.canonicalPayload()
		if err != nil {
			if markErr := d.markFailed(ctx, record, err); markErr != nil {
				return markErr
			}
			continue
		}
		if err := d.publisher.PublishLanguageConfigChanged(ctx, payload); err != nil {
			if markErr := d.markFailed(ctx, record, fmt.Errorf("publish language config event: %w", err)); markErr != nil {
				return markErr
			}
			continue
		}
		if err := d.repository.MarkLanguageConfigOutboxPublished(ctx, record.ID); err != nil {
			return fmt.Errorf("mark language config outbox published: %w", err)
		}
	}
	return nil
}

func (d *LanguageConfigOutboxDispatcher) validate() error {
	if d == nil || d.repository == nil || d.publisher == nil {
		return ErrLanguageConfigOutboxUnavailable
	}
	return nil
}

func (d *LanguageConfigOutboxDispatcher) dispatch(ctx context.Context) {
	if err := d.DispatchOnce(ctx); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) && d.logger != nil {
		d.logger.Warn("language config outbox dispatch failed; will retry", "error", err)
	}
}

func (d *LanguageConfigOutboxDispatcher) markFailed(ctx context.Context, record LanguageConfigOutboxRecord, cause error) error {
	availableAt := d.now().Add(languageConfigOutboxRetryDelay(record.Attempts)).UTC()
	if err := d.repository.MarkLanguageConfigOutboxFailed(ctx, record.ID, cause.Error(), availableAt); err != nil {
		return fmt.Errorf("mark language config outbox failed: %w", err)
	}
	return nil
}

func languageConfigOutboxRetryDelay(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	delay := time.Second
	for range attempts - 1 {
		if delay >= maxLanguageConfigOutboxRetryDelay/2 {
			return maxLanguageConfigOutboxRetryDelay
		}
		delay *= 2
	}
	return delay
}
