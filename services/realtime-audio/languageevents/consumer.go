package languageevents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	realtimev1 "github.com/1024XEngineer/xe6-tsy/packages/contracts/realtime/v1"
	"github.com/1024XEngineer/xe6-tsy/services/realtime-audio/speech"
	"golang.org/x/text/language"
)

var (
	ErrConsumerRequired = errors.New("language config consumer is required")
	ErrPreparerRequired = errors.New("language config binding preparer is required")
	ErrInvalidEvent     = errors.New("invalid language config changed event")
	ErrEventConflict    = errors.New("conflicting language config changed event")
	ErrEventPairInvalid = errors.New("language config event does not describe one language pair")
)

const consumerRetryDelay = time.Second

// BindingPreparer is implemented by the realtime BindingCoordinator. The
// consumer deliberately depends on this narrow operation so broker replay and
// provider selection remain independently testable.
type BindingPreparer interface {
	Prepare(context.Context, string, int64, string, string) error
}

type eventTask struct {
	event    realtimev1.LanguageConfigChangedEvent
	pairA    string
	pairB    string
	receipts map[string]struct{}
}

type sessionEventState struct {
	highestVersion int64
	eventID        string
	pairKey        string
	prepared       bool
	settled        bool
	pending        *eventTask
}

// Consumer validates and applies language configuration events. State is
// process-local: durable stream replay recovers process restarts, while the
// monotonic version fence prevents an older replay from replacing a newer
// binding. One pending task owns all duplicate receipts for a session/version.
type Consumer struct {
	stream   StreamConsumer
	preparer BindingPreparer
	logger   *slog.Logger

	mu       sync.Mutex
	sessions map[string]sessionEventState
	tasks    sync.WaitGroup
}

// NewConsumer constructs a realtime language-config event consumer.
func NewConsumer(stream StreamConsumer, preparer BindingPreparer, logger *slog.Logger) (*Consumer, error) {
	if stream == nil {
		return nil, ErrConsumerRequired
	}
	if preparer == nil {
		return nil, ErrPreparerRequired
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Consumer{
		stream: stream, preparer: preparer, logger: logger,
		sessions: make(map[string]sessionEventState),
	}, nil
}

// Run receives until cancellation. Preparation runs in tracked background
// tasks, so a slow vN+1 route lookup cannot block receipt of vN+2. Canceling
// Run cancels every preparation and intentionally leaves unsettled receipts in
// the pending list for a later consumer to reclaim.
func (c *Consumer) Run(ctx context.Context) error {
	if err := c.validate(); err != nil {
		return err
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	for {
		message, err := c.stream.Receive(runCtx)
		if err != nil {
			if runCtx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				c.tasks.Wait()
				return nil
			}
			c.logger.Warn("language config stream receive failed; retrying", "error", err)
			if !waitForRetry(runCtx) {
				c.tasks.Wait()
				return nil
			}
			continue
		}
		if err := c.schedule(runCtx, message); err != nil && runCtx.Err() == nil {
			c.logger.Warn("language config event scheduling failed", "error", err)
		}
	}
}

// ProcessOnce receives and settles one entry synchronously. It is intended for
// deterministic tests and one-shot operational probes; Run is the production
// asynchronous path.
func (c *Consumer) ProcessOnce(ctx context.Context) (bool, error) {
	if err := c.validate(); err != nil {
		return false, err
	}
	message, err := c.stream.Receive(ctx)
	if err != nil {
		return false, err
	}
	return true, c.process(ctx, message, false)
}

func (c *Consumer) schedule(ctx context.Context, message StreamMessage) error {
	return c.process(ctx, message, true)
}

func (c *Consumer) process(ctx context.Context, message StreamMessage, asynchronous bool) error {
	event, pairA, pairB, err := decodeEvent(message.Payload)
	if err != nil {
		return c.ack(ctx, message.Receipt, err)
	}

	action, task := c.accept(event, pairA, pairB, message.Receipt)
	switch action {
	case eventAck:
		return c.ack(ctx, message.Receipt, nil)
	case eventConflict:
		return c.ack(ctx, message.Receipt, ErrEventConflict)
	case eventJoin:
		return nil
	case eventPrepare:
		if asynchronous {
			c.tasks.Add(1)
			go func() {
				defer c.tasks.Done()
				if err := c.prepareAndSettle(ctx, task); err != nil && ctx.Err() == nil {
					c.logger.Warn("language config event preparation deferred",
						"event_id", task.event.EventID,
						"session_id", task.event.SessionID,
						"language_config_version", task.event.LanguageConfigVersion,
						"error", err,
					)
				}
			}()
			return nil
		}
		return c.prepareAndSettle(ctx, task)
	default:
		return fmt.Errorf("unknown language config event action %d", action)
	}
}

func (c *Consumer) validate() error {
	if c == nil || c.stream == nil {
		return ErrConsumerRequired
	}
	if c.preparer == nil {
		return ErrPreparerRequired
	}
	return nil
}

type eventAction uint8

const (
	eventAck eventAction = iota
	eventPrepare
	eventJoin
	eventConflict
)

// accept applies the event-id and version fences while holding the state lock.
// A duplicate receipt joins the existing task instead of starting a second
// prepare or being ACKed prematurely before the first prepare succeeds.
func (c *Consumer) accept(
	event realtimev1.LanguageConfigChangedEvent,
	pairA, pairB, receipt string,
) (eventAction, *eventTask) {
	c.mu.Lock()
	defer c.mu.Unlock()

	state := c.sessions[event.SessionID]
	if state.highestVersion > event.LanguageConfigVersion {
		return eventAck, nil
	}
	if state.highestVersion == event.LanguageConfigVersion {
		if state.eventID != event.EventID {
			return eventConflict, nil
		}
		if state.pairKey != pairKey(pairA, pairB) {
			return eventConflict, nil
		}
		if state.pending != nil {
			addReceipt(state.pending, receipt)
			return eventJoin, nil
		}
		if state.settled {
			return eventAck, nil
		}
	}

	task := &eventTask{
		event: event, pairA: pairA, pairB: pairB,
		receipts: make(map[string]struct{}, 1),
	}
	addReceipt(task, receipt)
	state.highestVersion = event.LanguageConfigVersion
	state.eventID = event.EventID
	state.pairKey = pairKey(pairA, pairB)
	state.prepared = false
	state.settled = false
	state.pending = task
	c.sessions[event.SessionID] = state
	return eventPrepare, task
}

func (c *Consumer) prepareAndSettle(ctx context.Context, task *eventTask) error {
	prepareErr := c.preparer.Prepare(
		ctx,
		task.event.SessionID,
		task.event.LanguageConfigVersion,
		task.pairA,
		task.pairB,
	)
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if !c.current(task) {
		return c.ackReceipts(ctx, task, nil)
	}
	if prepareErr != nil {
		if isPermanentPreparationError(prepareErr) {
			c.finish(task, false, true)
			return c.ackReceipts(ctx, task, prepareErr)
		}
		c.finish(task, false, false)
		return c.nackReceipts(ctx, task, prepareErr)
	}

	c.finish(task, true, true)
	return c.ackReceipts(ctx, task, nil)
}

func (c *Consumer) current(task *eventTask) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	state, ok := c.sessions[task.event.SessionID]
	return ok && state.pending == task && state.highestVersion == task.event.LanguageConfigVersion && state.eventID == task.event.EventID
}

func (c *Consumer) finish(task *eventTask, prepared, settled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	state, ok := c.sessions[task.event.SessionID]
	if !ok || state.pending != task {
		return
	}
	state.pending = nil
	state.prepared = prepared
	state.settled = settled
	c.sessions[task.event.SessionID] = state
}

func (c *Consumer) ackReceipts(ctx context.Context, task *eventTask, cause error) error {
	for _, receipt := range c.taskReceipts(task) {
		if err := c.stream.Ack(ctx, receipt); err != nil {
			cause = errors.Join(cause, err)
		}
	}
	return cause
}

func (c *Consumer) nackReceipts(ctx context.Context, task *eventTask, cause error) error {
	for _, receipt := range c.taskReceipts(task) {
		if err := c.stream.Nack(ctx, receipt); err != nil {
			cause = errors.Join(cause, err)
		}
	}
	return cause
}

func (c *Consumer) ack(ctx context.Context, receipt string, cause error) error {
	if err := c.stream.Ack(ctx, receipt); err != nil {
		return errors.Join(cause, err)
	}
	// Invalid or conflicting entries are permanently discarded once the
	// broker ACK succeeds; returning the validation cause would make callers
	// treat a successfully settled receipt as a retryable receive failure.
	return nil
}

func addReceipt(task *eventTask, receipt string) {
	if task == nil || strings.TrimSpace(receipt) == "" {
		return
	}
	task.receipts[receipt] = struct{}{}
}

func (c *Consumer) taskReceipts(task *eventTask) []string {
	if task == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	receipts := make([]string, 0, len(task.receipts))
	for receipt := range task.receipts {
		receipts = append(receipts, receipt)
	}
	return receipts
}

func decodeEvent(payload []byte) (realtimev1.LanguageConfigChangedEvent, string, string, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var event realtimev1.LanguageConfigChangedEvent
	if err := decoder.Decode(&event); err != nil {
		return realtimev1.LanguageConfigChangedEvent{}, "", "", fmt.Errorf("%w: decode payload: %v", ErrInvalidEvent, err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return realtimev1.LanguageConfigChangedEvent{}, "", "", fmt.Errorf("%w: trailing JSON value", ErrInvalidEvent)
		}
		return realtimev1.LanguageConfigChangedEvent{}, "", "", fmt.Errorf("%w: decode trailing JSON: %v", ErrInvalidEvent, err)
	}
	if err := event.Validate(); err != nil {
		return realtimev1.LanguageConfigChangedEvent{}, "", "", fmt.Errorf("%w: %w", ErrInvalidEvent, err)
	}
	pairA, pairB, err := eventPair(event.LanguagePairs)
	if err != nil {
		return realtimev1.LanguageConfigChangedEvent{}, "", "", fmt.Errorf("%w: %v", ErrInvalidEvent, err)
	}
	return event, pairA, pairB, nil
}

func eventPair(pairs []realtimev1.LanguageConfigPair) (string, string, error) {
	if len(pairs) != 2 {
		return "", "", ErrEventPairInvalid
	}
	firstA, firstB, err := canonicalPair(pairs[0].Source, pairs[0].Target)
	if err != nil {
		return "", "", err
	}
	secondA, secondB, err := canonicalPair(pairs[1].Source, pairs[1].Target)
	if err != nil || firstA != secondA || firstB != secondB {
		return "", "", ErrEventPairInvalid
	}
	return firstA, firstB, nil
}

func canonicalPair(first, second string) (string, string, error) {
	firstTag, err := canonicalLanguage(first)
	if err != nil {
		return "", "", err
	}
	secondTag, err := canonicalLanguage(second)
	if err != nil {
		return "", "", err
	}
	if firstTag == secondTag {
		return "", "", ErrEventPairInvalid
	}
	if firstTag > secondTag {
		firstTag, secondTag = secondTag, firstTag
	}
	return firstTag, secondTag, nil
}

func canonicalLanguage(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ErrEventPairInvalid
	}
	tag, err := language.Parse(strings.ReplaceAll(value, "_", "-"))
	if err != nil || tag == language.Und {
		return "", ErrEventPairInvalid
	}
	return tag.String(), nil
}

func pairKey(first, second string) string {
	return first + "\x00" + second
}

func isPermanentPreparationError(err error) bool {
	return errors.Is(err, speech.ErrLanguageRequired) ||
		errors.Is(err, speech.ErrLanguageInvalid) ||
		errors.Is(err, speech.ErrLanguagePairInvalid) ||
		errors.Is(err, speech.ErrSpeechRouteNotFound) ||
		errors.Is(err, speech.ErrSpeechRouteInvalid) ||
		errors.Is(err, speech.ErrSpeechRouteMismatch) ||
		errors.Is(err, speech.ErrASRProfileNotFound) ||
		errors.Is(err, speech.ErrTTSProfileNotFound) ||
		errors.Is(err, speech.ErrBindingPreparationConflict) ||
		errors.Is(err, speech.ErrBindingVersionConflict) ||
		errors.Is(err, speech.ErrBindingSessionClosed)
}

func waitForRetry(ctx context.Context) bool {
	timer := time.NewTimer(consumerRetryDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
