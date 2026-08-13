package languageevents

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	realtimev1 "github.com/1024XEngineer/xe6-tsy/packages/contracts/realtime/v1"
	"github.com/1024XEngineer/xe6-tsy/services/realtime-audio/asr"
	"github.com/1024XEngineer/xe6-tsy/services/realtime-audio/speech"
	"github.com/1024XEngineer/xe6-tsy/services/realtime-audio/tts"
)

func TestConsumerRejectsMalformedPayloadAndAcknowledgesIt(t *testing.T) {
	valid := marshalEvent(t, testEvent("event-1", 1))
	tests := []struct {
		name    string
		payload []byte
	}{
		{name: "malformed", payload: []byte("{")},
		{name: "unknown field", payload: append(valid[:len(valid)-1], []byte(`,"unknown":true}`)...)},
		{name: "trailing value", payload: append(valid, []byte(`{}`)...)},
		{name: "invalid contract", payload: []byte(`{"event_version":1}`)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stream := &recordingStream{messages: []StreamMessage{{Payload: test.payload, Receipt: "receipt"}}}
			preparer := &recordingPreparer{}
			consumer := mustConsumer(t, stream, preparer)
			processed, err := consumer.ProcessOnce(t.Context())
			if !processed || err != nil {
				t.Fatalf("ProcessOnce() = (%v, %v), want (true, nil)", processed, err)
			}
			if len(stream.ackedReceipts()) != 1 || len(preparer.calls()) != 0 {
				t.Fatalf("acked=%v prepares=%v", stream.ackedReceipts(), preparer.calls())
			}
		})
	}
}

func TestConsumerPreparesValidEventAndAcknowledges(t *testing.T) {
	stream := &recordingStream{messages: []StreamMessage{{Payload: marshalEvent(t, testEvent("event-1", 1)), Receipt: "receipt-1"}}}
	preparer := &recordingPreparer{}
	consumer := mustConsumer(t, stream, preparer)

	processed, err := consumer.ProcessOnce(t.Context())
	if !processed || err != nil {
		t.Fatalf("ProcessOnce() = (%v, %v), want (true, nil)", processed, err)
	}
	calls := preparer.calls()
	if len(calls) != 1 || calls[0].sessionID != "session-1" || calls[0].version != 1 || calls[0].languageA != "en-US" || calls[0].languageB != "zh-CN" {
		t.Fatalf("prepare calls = %#v", calls)
	}
	if got := stream.ackedReceipts(); len(got) != 1 || got[0] != "receipt-1" {
		t.Fatalf("acked receipts = %v", got)
	}
}

func TestConsumerDuplicateReceiptJoinsPendingPreparation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stream := &recordingStream{}
		preparer := &blockingPreparer{started: make(chan struct{}), release: make(chan struct{})}
		consumer := mustConsumer(t, stream, preparer)
		first := StreamMessage{Payload: marshalEvent(t, testEvent("event-1", 1)), Receipt: "receipt-1"}
		second := StreamMessage{Payload: first.Payload, Receipt: "receipt-2"}

		if err := consumer.schedule(t.Context(), first); err != nil {
			t.Fatalf("schedule(first) error = %v", err)
		}
		<-preparer.started
		if err := consumer.schedule(t.Context(), second); err != nil {
			t.Fatalf("schedule(duplicate) error = %v", err)
		}
		if got := len(preparer.calls()); got != 1 {
			t.Fatalf("prepare calls = %d, want one shared call", got)
		}
		close(preparer.release)
		consumer.tasks.Wait()
		acked := stream.ackedReceipts()
		if len(acked) != 2 || !containsString(acked, "receipt-1") || !containsString(acked, "receipt-2") {
			t.Fatalf("acked receipts = %v, want both duplicate receipts", acked)
		}
		if got := len(stream.nackedReceipts()); got != 0 {
			t.Fatalf("nacked receipts = %v", stream.nackedReceipts())
		}
	})
}

func TestConsumerTransientPreparationFailureLeavesAllReceiptsPendingAndRetries(t *testing.T) {
	stream := &recordingStream{}
	preparer := &recordingPreparer{errs: []error{errors.New("catalog temporarily unavailable")}}
	consumer := mustConsumer(t, stream, preparer)
	event := marshalEvent(t, testEvent("event-1", 1))
	stream.push(StreamMessage{Payload: event, Receipt: "receipt-1"})
	if _, err := consumer.ProcessOnce(t.Context()); err == nil {
		t.Fatal("ProcessOnce() error = nil, want transient preparation error")
	}
	if len(stream.ackedReceipts()) != 0 || len(stream.nackedReceipts()) != 1 {
		t.Fatalf("acked=%v nacked=%v, want pending retry", stream.ackedReceipts(), stream.nackedReceipts())
	}

	preparer.setErrs(nil)
	stream.push(StreamMessage{Payload: event, Receipt: "receipt-2"})
	if _, err := consumer.ProcessOnce(t.Context()); err != nil {
		t.Fatalf("retry ProcessOnce() error = %v", err)
	}
	if len(preparer.calls()) != 2 {
		t.Fatalf("prepare calls = %d, want initial plus retry", len(preparer.calls()))
	}
	if got := stream.ackedReceipts(); len(got) != 1 || got[0] != "receipt-2" {
		t.Fatalf("acked receipts = %v, want only successful retry receipt", got)
	}
}

func TestConsumerAcknowledgesOlderEventAfterNewerVersion(t *testing.T) {
	stream := &recordingStream{}
	preparer := &recordingPreparer{}
	consumer := mustConsumer(t, stream, preparer)
	newer := StreamMessage{Payload: marshalEvent(t, testEvent("event-2", 2)), Receipt: "newer"}
	older := StreamMessage{Payload: marshalEvent(t, testEvent("event-1", 1)), Receipt: "older"}
	stream.push(newer)
	if _, err := consumer.ProcessOnce(t.Context()); err != nil {
		t.Fatalf("newer ProcessOnce() error = %v", err)
	}
	stream.push(older)
	if _, err := consumer.ProcessOnce(t.Context()); err != nil {
		t.Fatalf("older ProcessOnce() error = %v", err)
	}
	if got := len(preparer.calls()); got != 1 {
		t.Fatalf("prepare calls = %d, want one newer preparation", got)
	}
	acked := stream.ackedReceipts()
	if len(acked) != 2 || !containsString(acked, "newer") || !containsString(acked, "older") {
		t.Fatalf("acked receipts = %v", acked)
	}
}

func TestConsumerRejectsSameVersionConflictingEvent(t *testing.T) {
	stream := &recordingStream{}
	preparer := &recordingPreparer{}
	consumer := mustConsumer(t, stream, preparer)
	first := testEvent("event-1", 3)
	conflict := testEvent("event-2", 3)
	conflict.LanguagePairs[0].Source = "ja-JP"
	conflict.LanguagePairs[0].Target = "en-US"
	conflict.LanguagePairs[1].Source = "en-US"
	conflict.LanguagePairs[1].Target = "ja-JP"
	stream.push(StreamMessage{Payload: marshalEvent(t, first), Receipt: "first"})
	stream.push(StreamMessage{Payload: marshalEvent(t, conflict), Receipt: "conflict"})
	if _, err := consumer.ProcessOnce(t.Context()); err != nil {
		t.Fatalf("first ProcessOnce() error = %v", err)
	}
	if _, err := consumer.ProcessOnce(t.Context()); err != nil {
		t.Fatalf("conflict ProcessOnce() error = %v, permanent conflict should be acked", err)
	}
	if len(preparer.calls()) != 1 || !containsString(stream.ackedReceipts(), "conflict") {
		t.Fatalf("prepares=%v acked=%v", preparer.calls(), stream.ackedReceipts())
	}
}

func TestConsumerAsynchronousNewerEventDoesNotGetOverwrittenByOlderCompletion(t *testing.T) {
	stream := &recordingStream{}
	preparer := &versionBlockingPreparer{
		started: map[int64]chan struct{}{1: make(chan struct{}), 2: make(chan struct{})},
		release: map[int64]chan struct{}{1: make(chan struct{}), 2: make(chan struct{})},
	}
	consumer := mustConsumer(t, stream, preparer)
	if err := consumer.schedule(t.Context(), StreamMessage{Payload: marshalEvent(t, testEvent("event-1", 1)), Receipt: "v1"}); err != nil {
		t.Fatalf("schedule(v1) error = %v", err)
	}
	<-preparer.started[1]
	if err := consumer.schedule(t.Context(), StreamMessage{Payload: marshalEvent(t, testEvent("event-2", 2)), Receipt: "v2"}); err != nil {
		t.Fatalf("schedule(v2) error = %v", err)
	}
	<-preparer.started[2]
	close(preparer.release[2])
	close(preparer.release[1])
	consumer.tasks.Wait()
	if got := preparer.versions(); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("prepare versions = %v", got)
	}
	acked := stream.ackedReceipts()
	if len(acked) != 2 || !containsString(acked, "v1") || !containsString(acked, "v2") {
		t.Fatalf("acked receipts = %v", acked)
	}
}

func TestConsumerPreservesNewerCoordinatorBindingAfterLateResolverReturns(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		resolver := &lateResolver{
			started: []chan struct{}{make(chan struct{}), make(chan struct{})},
			release: []chan struct{}{make(chan struct{}), make(chan struct{})},
			routes: []speech.SpeechRoute{
				{LanguageA: "en-US", LanguageB: "zh-CN", ASRProfileID: "asr-zh", TTSProfileID: "tts-zh"},
				{LanguageA: "en-US", LanguageB: "ja-JP", ASRProfileID: "asr-ja", TTSProfileID: "tts-ja"},
			},
		}
		registry, err := speech.NewProviderRegistry(
			[]speech.ASRProfile{
				{Profile: speech.Profile{ID: "asr-zh"}, Adapter: asr.NewFakeProvider(asr.FakeProviderConfig{})},
				{Profile: speech.Profile{ID: "asr-ja"}, Adapter: asr.NewFakeProvider(asr.FakeProviderConfig{})},
			},
			[]speech.TTSProfile{
				{Profile: speech.Profile{ID: "tts-zh"}, Adapter: tts.NewFakeProvider(tts.FakeProviderConfig{})},
				{Profile: speech.Profile{ID: "tts-ja"}, Adapter: tts.NewFakeProvider(tts.FakeProviderConfig{})},
			},
		)
		if err != nil {
			t.Fatalf("NewProviderRegistry() error = %v", err)
		}
		coordinator, err := speech.NewBindingCoordinator(registry, resolver)
		if err != nil {
			t.Fatalf("NewBindingCoordinator() error = %v", err)
		}
		consumer := mustConsumer(t, &recordingStream{}, coordinator)

		if err := consumer.schedule(t.Context(), StreamMessage{Payload: marshalEvent(t, testEvent("event-1", 1)), Receipt: "v1"}); err != nil {
			t.Fatalf("schedule(v1) error = %v", err)
		}
		<-resolver.started[0]
		if err := consumer.schedule(t.Context(), StreamMessage{Payload: marshalEvent(t, testEventWithPair("event-2", 2, "ja-JP", "en-US")), Receipt: "v2"}); err != nil {
			t.Fatalf("schedule(v2) error = %v", err)
		}
		<-resolver.started[1]

		close(resolver.release[1])
		synctest.Wait()
		assertCoordinatorBinding(t, coordinator, 2, "asr-ja", "tts-ja")

		close(resolver.release[0])
		consumer.tasks.Wait()
		assertCoordinatorBinding(t, coordinator, 2, "asr-ja", "tts-ja")
		if _, _, err := coordinator.AcquireForTurn(t.Context(), "session-1", 1); !errors.Is(err, speech.ErrBindingVersionConflict) {
			t.Fatalf("AcquireForTurn(version 1) error = %v, want version conflict", err)
		}
	})
}

func assertCoordinatorBinding(t *testing.T, coordinator *speech.BindingCoordinator, version int64, asrProfileID, ttsProfileID string) {
	t.Helper()
	binding, release, err := coordinator.AcquireForTurn(t.Context(), "session-1", version)
	if err != nil {
		t.Fatalf("AcquireForTurn(version %d) error = %v", version, err)
	}
	defer release()
	if binding.Route.ASRProfileID != asrProfileID || binding.Route.TTSProfileID != ttsProfileID {
		t.Fatalf("binding = %#v, want ASR %q and TTS %q", binding.Route, asrProfileID, ttsProfileID)
	}
}

func mustConsumer(t *testing.T, stream StreamConsumer, preparer BindingPreparer) *Consumer {
	t.Helper()
	consumer, err := NewConsumer(stream, preparer, nil)
	if err != nil {
		t.Fatalf("NewConsumer() error = %v", err)
	}
	return consumer
}

func testEvent(eventID string, version int64) realtimev1.LanguageConfigChangedEvent {
	return testEventWithPair(eventID, version, "zh-CN", "en-US")
}

func testEventWithPair(eventID string, version int64, first, second string) realtimev1.LanguageConfigChangedEvent {
	return realtimev1.LanguageConfigChangedEvent{
		EventVersion: realtimev1.LanguageConfigChangedEventVersion,
		EventID:      eventID, TraceID: "trace-" + eventID, SessionID: "session-1",
		LanguageConfigVersion: version,
		LanguagePairs: []realtimev1.LanguageConfigPair{
			{Source: first, Target: second}, {Source: second, Target: first},
		},
		OutputRoutes: []realtimev1.LanguageConfigOutputRoute{{TargetLanguage: second, TTSEnabled: true}},
		OccurredAt:   time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC),
	}
}

func marshalEvent(t *testing.T, event realtimev1.LanguageConfigChangedEvent) []byte {
	t.Helper()
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return payload
}

type prepareCall struct {
	sessionID string
	version   int64
	languageA string
	languageB string
}

type recordingPreparer struct {
	mu        sync.Mutex
	callsList []prepareCall
	errs      []error
}

func (p *recordingPreparer) Prepare(_ context.Context, sessionID string, version int64, languageA, languageB string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.callsList = append(p.callsList, prepareCall{sessionID: sessionID, version: version, languageA: languageA, languageB: languageB})
	if len(p.errs) > 0 {
		err := p.errs[0]
		p.errs = p.errs[1:]
		return err
	}
	return nil
}

func (p *recordingPreparer) calls() []prepareCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]prepareCall(nil), p.callsList...)
}

func (p *recordingPreparer) setErrs(errs []error) {
	p.mu.Lock()
	p.errs = append([]error(nil), errs...)
	p.mu.Unlock()
}

type blockingPreparer struct {
	started  chan struct{}
	release  chan struct{}
	recorder recordingPreparer
}

func (p *blockingPreparer) Prepare(ctx context.Context, sessionID string, version int64, languageA, languageB string) error {
	p.recorder.mu.Lock()
	p.recorder.callsList = append(p.recorder.callsList, prepareCall{sessionID: sessionID, version: version, languageA: languageA, languageB: languageB})
	p.recorder.mu.Unlock()
	select {
	case <-p.started:
	default:
		close(p.started)
	}
	select {
	case <-p.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *blockingPreparer) calls() []prepareCall { return p.recorder.calls() }

type versionBlockingPreparer struct {
	mu      sync.Mutex
	started map[int64]chan struct{}
	release map[int64]chan struct{}
	seen    []int64
}

func (p *versionBlockingPreparer) Prepare(ctx context.Context, _ string, version int64, _, _ string) error {
	p.mu.Lock()
	p.seen = append(p.seen, version)
	started := p.started[version]
	release := p.release[version]
	p.mu.Unlock()
	close(started)
	select {
	case <-release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *versionBlockingPreparer) versions() []int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]int64(nil), p.seen...)
}

type lateResolver struct {
	mu      sync.Mutex
	started []chan struct{}
	release []chan struct{}
	routes  []speech.SpeechRoute
	calls   int
}

func (r *lateResolver) ResolveBinding(_ context.Context, _, _ string) (speech.SpeechRoute, error) {
	r.mu.Lock()
	index := r.calls
	r.calls++
	started := r.started[index]
	release := r.release[index]
	route := r.routes[index]
	r.mu.Unlock()
	close(started)
	<-release
	return route, nil
}

type recordingStream struct {
	mu       sync.Mutex
	messages []StreamMessage
	acked    []string
	nacked   []string
}

func (s *recordingStream) Receive(ctx context.Context) (StreamMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.messages) == 0 {
		return StreamMessage{}, ctx.Err()
	}
	message := s.messages[0]
	s.messages = s.messages[1:]
	return message, nil
}

func (s *recordingStream) Ack(_ context.Context, receipt string) error {
	s.mu.Lock()
	s.acked = append(s.acked, receipt)
	s.mu.Unlock()
	return nil
}

func (s *recordingStream) Nack(_ context.Context, receipt string) error {
	s.mu.Lock()
	s.nacked = append(s.nacked, receipt)
	s.mu.Unlock()
	return nil
}

func (s *recordingStream) push(message StreamMessage) {
	s.mu.Lock()
	s.messages = append(s.messages, message)
	s.mu.Unlock()
}

func (s *recordingStream) ackedReceipts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.acked...)
}

func (s *recordingStream) nackedReceipts() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.nacked...)
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

var _ BindingPreparer = (*speech.BindingCoordinator)(nil)
