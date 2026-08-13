package languages

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	realtimev1 "github.com/1024XEngineer/xe6-tsy/packages/contracts/realtime/v1"
)

type languageConfigOutboxRepositoryStub struct {
	records      []LanguageConfigOutboxRecord
	claimErr     error
	published    []string
	publishedErr error
	failed       []languageConfigOutboxFailure
	failedErr    error
}

type languageConfigOutboxFailure struct {
	id          string
	reason      string
	availableAt time.Time
}

func (s *languageConfigOutboxRepositoryStub) ClaimLanguageConfigOutbox(context.Context, int) ([]LanguageConfigOutboxRecord, error) {
	if s.claimErr != nil {
		return nil, s.claimErr
	}
	return s.records, nil
}

func (s *languageConfigOutboxRepositoryStub) MarkLanguageConfigOutboxPublished(_ context.Context, id string) error {
	s.published = append(s.published, id)
	return s.publishedErr
}

func (s *languageConfigOutboxRepositoryStub) MarkLanguageConfigOutboxFailed(_ context.Context, id, reason string, availableAt time.Time) error {
	s.failed = append(s.failed, languageConfigOutboxFailure{id: id, reason: reason, availableAt: availableAt})
	return s.failedErr
}

type languageConfigChangedPublisherStub struct {
	payloads   [][]byte
	err        error
	onPublish  func()
	publishErr func([]byte) error
}

func (s *languageConfigChangedPublisherStub) PublishLanguageConfigChanged(_ context.Context, payload []byte) error {
	s.payloads = append(s.payloads, append([]byte(nil), payload...))
	if s.onPublish != nil {
		s.onPublish()
	}
	if s.publishErr != nil {
		return s.publishErr(payload)
	}
	return s.err
}

func TestLanguageConfigOutboxDispatcherPublishesBeforeMarkingPublished(t *testing.T) {
	record := testLanguageConfigOutboxRecord(t, 1)
	repository := &languageConfigOutboxRepositoryStub{records: []LanguageConfigOutboxRecord{record}}
	publishedAtPublish := -1
	publisher := &languageConfigChangedPublisherStub{
		onPublish: func() {
			publishedAtPublish = len(repository.published)
		},
	}
	dispatcher := NewLanguageConfigOutboxDispatcher(repository, publisher, time.Second, nil)

	if err := dispatcher.DispatchOnce(t.Context()); err != nil {
		t.Fatalf("DispatchOnce() error = %v", err)
	}
	if len(publisher.payloads) != 1 || string(publisher.payloads[0]) != string(record.Payload) {
		t.Fatalf("published payloads = %#v, want exact record payload", publisher.payloads)
	}
	if len(repository.published) != 1 || repository.published[0] != record.ID {
		t.Fatalf("published records = %#v, want %q", repository.published, record.ID)
	}
	if publishedAtPublish != 0 {
		t.Fatalf("records marked published while broker append ran = %d, want 0", publishedAtPublish)
	}
	if len(repository.failed) != 0 {
		t.Fatalf("failed records = %#v, want none", repository.failed)
	}
}

func TestLanguageConfigOutboxDispatcherLeavesFailedPublicationPending(t *testing.T) {
	record := testLanguageConfigOutboxRecord(t, 3)
	repository := &languageConfigOutboxRepositoryStub{records: []LanguageConfigOutboxRecord{record}}
	publisher := &languageConfigChangedPublisherStub{err: errors.New("Valkey unavailable")}
	dispatcher := NewLanguageConfigOutboxDispatcher(repository, publisher, time.Second, nil)
	now := time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC)
	dispatcher.now = func() time.Time { return now }

	if err := dispatcher.DispatchOnce(t.Context()); err != nil {
		t.Fatalf("DispatchOnce() error = %v", err)
	}
	if len(repository.published) != 0 {
		t.Fatalf("published records = %#v, want none", repository.published)
	}
	if len(repository.failed) != 1 {
		t.Fatalf("failed records = %#v, want one", repository.failed)
	}
	failed := repository.failed[0]
	if failed.id != record.ID || !strings.Contains(failed.reason, "Valkey unavailable") {
		t.Fatalf("failure = %#v, want record %q and broker reason", failed, record.ID)
	}
	if want := now.Add(4 * time.Second); !failed.availableAt.Equal(want) {
		t.Fatalf("retry at = %s, want %s", failed.availableAt, want)
	}
}

func TestLanguageConfigOutboxDispatcherContinuesAfterFailedPublication(t *testing.T) {
	failed := testLanguageConfigOutboxRecordFor(t, "failed", 1)
	succeeded := testLanguageConfigOutboxRecordFor(t, "succeeded", 1)
	repository := &languageConfigOutboxRepositoryStub{records: []LanguageConfigOutboxRecord{failed, succeeded}}
	publisher := &languageConfigChangedPublisherStub{
		publishErr: func(payload []byte) error {
			if string(payload) == string(failed.Payload) {
				return errors.New("Valkey unavailable")
			}
			return nil
		},
	}
	dispatcher := NewLanguageConfigOutboxDispatcher(repository, publisher, time.Second, nil)

	if err := dispatcher.DispatchOnce(t.Context()); err != nil {
		t.Fatalf("DispatchOnce() error = %v", err)
	}
	if len(publisher.payloads) != 2 {
		t.Fatalf("published payload count = %d, want 2", len(publisher.payloads))
	}
	if len(repository.failed) != 1 || repository.failed[0].id != failed.ID {
		t.Fatalf("failed records = %#v, want only %q", repository.failed, failed.ID)
	}
	if len(repository.published) != 1 || repository.published[0] != succeeded.ID {
		t.Fatalf("published records = %#v, want only %q", repository.published, succeeded.ID)
	}
}

func TestLanguageConfigOutboxDispatcherDoesNotPublishInvalidPayload(t *testing.T) {
	record := testLanguageConfigOutboxRecord(t, 1)
	var event realtimev1.LanguageConfigChangedEvent
	if err := json.Unmarshal(record.Payload, &event); err != nil {
		t.Fatalf("decode canonical payload: %v", err)
	}
	event.TraceID = "tampered-trace"
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal tampered payload: %v", err)
	}
	record.Payload = payload
	repository := &languageConfigOutboxRepositoryStub{records: []LanguageConfigOutboxRecord{record}}
	publisher := &languageConfigChangedPublisherStub{}
	dispatcher := NewLanguageConfigOutboxDispatcher(repository, publisher, time.Second, nil)

	if err := dispatcher.DispatchOnce(t.Context()); err != nil {
		t.Fatalf("DispatchOnce() error = %v", err)
	}
	if len(publisher.payloads) != 0 || len(repository.published) != 0 {
		t.Fatalf("invalid payload was published: payloads=%d marked=%d", len(publisher.payloads), len(repository.published))
	}
	if len(repository.failed) != 1 || !strings.Contains(repository.failed[0].reason, "payload hash mismatch") {
		t.Fatalf("failure = %#v, want payload hash mismatch", repository.failed)
	}
}

func TestLanguageConfigOutboxDispatcherCanonicalizesJSONBPayloadBeforeHashing(t *testing.T) {
	record := testLanguageConfigOutboxRecord(t, 1)
	wantPayload := append([]byte(nil), record.Payload...)
	var document map[string]any
	if err := json.Unmarshal(record.Payload, &document); err != nil {
		t.Fatalf("decode canonical payload: %v", err)
	}
	reorderedPayload, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("marshal JSONB-shaped payload: %v", err)
	}
	if string(reorderedPayload) == string(wantPayload) {
		t.Fatal("test setup did not change JSON object field order")
	}
	record.Payload = reorderedPayload
	repository := &languageConfigOutboxRepositoryStub{records: []LanguageConfigOutboxRecord{record}}
	publisher := &languageConfigChangedPublisherStub{}
	dispatcher := NewLanguageConfigOutboxDispatcher(repository, publisher, time.Second, nil)

	if err := dispatcher.DispatchOnce(t.Context()); err != nil {
		t.Fatalf("DispatchOnce() error = %v", err)
	}
	if len(repository.failed) != 0 || len(repository.published) != 1 {
		t.Fatalf("settlement after JSONB reordering: failed=%#v published=%#v", repository.failed, repository.published)
	}
	if len(publisher.payloads) != 1 || string(publisher.payloads[0]) != string(wantPayload) {
		t.Fatalf("published payloads = %#v, want canonical payload %s", publisher.payloads, wantPayload)
	}
}

func TestLanguageConfigOutboxDispatcherReturnsFailurePersistenceError(t *testing.T) {
	record := testLanguageConfigOutboxRecord(t, 1)
	markErr := errors.New("Postgres unavailable")
	repository := &languageConfigOutboxRepositoryStub{
		records:   []LanguageConfigOutboxRecord{record},
		failedErr: markErr,
	}
	dispatcher := NewLanguageConfigOutboxDispatcher(repository, &languageConfigChangedPublisherStub{err: errors.New("Valkey unavailable")}, time.Second, nil)

	if err := dispatcher.DispatchOnce(t.Context()); !errors.Is(err, markErr) {
		t.Fatalf("DispatchOnce() error = %v, want %v", err, markErr)
	}
}

func TestLanguageConfigOutboxRetryDelayIsBounded(t *testing.T) {
	if got := languageConfigOutboxRetryDelay(1); got != time.Second {
		t.Fatalf("first retry delay = %s, want 1s", got)
	}
	if got := languageConfigOutboxRetryDelay(20); got != maxLanguageConfigOutboxRetryDelay {
		t.Fatalf("bounded retry delay = %s, want %s", got, maxLanguageConfigOutboxRetryDelay)
	}
}

func testLanguageConfigOutboxRecord(t *testing.T, attempts int) LanguageConfigOutboxRecord {
	return testLanguageConfigOutboxRecordFor(t, "1", attempts)
}

func testLanguageConfigOutboxRecordFor(t *testing.T, suffix string, attempts int) LanguageConfigOutboxRecord {
	t.Helper()
	event := realtimev1.LanguageConfigChangedEvent{
		EventVersion:          realtimev1.LanguageConfigChangedEventVersion,
		EventID:               "language-config:event-" + suffix,
		TraceID:               "trace-1",
		SessionID:             "session-" + suffix,
		LanguageConfigVersion: 1,
		LanguagePairs: []realtimev1.LanguageConfigPair{
			{Source: "zh-CN", Target: "en-US"},
		},
		OutputRoutes: []realtimev1.LanguageConfigOutputRoute{
			{TargetLanguage: "en-US", TTSEnabled: true},
		},
		OccurredAt: time.Date(2026, 8, 13, 8, 0, 0, 0, time.UTC),
	}
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	return LanguageConfigOutboxRecord{
		ID:          "language_config_outbox_" + suffix,
		EventID:     event.EventID,
		SessionID:   event.SessionID,
		Payload:     payload,
		PayloadHash: sha256.Sum256(payload),
		Attempts:    attempts,
	}
}
