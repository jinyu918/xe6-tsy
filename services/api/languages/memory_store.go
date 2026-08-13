package languages

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	realtimev1 "github.com/1024XEngineer/xe6-tsy/packages/contracts/realtime/v1"
	"github.com/oklog/ulid/v2"
)

// MemoryStore is an in-memory Store for unit tests (no database).
type MemoryStore struct {
	mu        sync.Mutex
	clock     Clock
	languages []SupportedLanguage
	configs   []LanguageConfig
	idempo    map[string]string // idempotency key -> config id
	outbox    []realtimev1.LanguageConfigChangedEvent
	failEvent error
}

func NewMemoryStore(clock Clock, languages []SupportedLanguage) *MemoryStore {
	if clock == nil {
		clock = systemClock{}
	}
	if languages == nil {
		languages = []SupportedLanguage{
			{LanguageCode: "zh-CN", DisplayName: "中文（简体）", DisplayNameEN: "Chinese (Simplified)", SupportsAsSource: true, SupportsAsTarget: true},
			{LanguageCode: "en-US", DisplayName: "English (US)", DisplayNameEN: "English (US)", SupportsAsSource: true, SupportsAsTarget: true},
		}
	}
	return &MemoryStore{
		clock:     clock,
		languages: append([]SupportedLanguage(nil), languages...),
		idempo:    make(map[string]string),
	}
}

func (s *MemoryStore) ListSupportedLanguages(_ context.Context, _ bool) ([]SupportedLanguage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]SupportedLanguage(nil), s.languages...), nil
}

func (s *MemoryStore) GetActiveConfig(_ context.Context, sessionID string) (LanguageConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.configs) - 1; i >= 0; i-- {
		cfg := s.configs[i]
		if cfg.SessionID == sessionID && cfg.Status == StatusActive {
			return cloneConfig(cfg), nil
		}
	}
	return LanguageConfig{}, ErrNoActiveConfig
}

func (s *MemoryStore) GetConfigByIdempotencyKey(_ context.Context, idempotencyKey string) (LanguageConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if idempotencyKey == "" {
		return LanguageConfig{}, ErrNoActiveConfig
	}
	id, ok := s.idempo[idempotencyKey]
	if !ok {
		return LanguageConfig{}, ErrNoActiveConfig
	}
	for _, cfg := range s.configs {
		if cfg.ID == id {
			return cloneConfig(cfg), nil
		}
	}
	return LanguageConfig{}, ErrNoActiveConfig
}

func (s *MemoryStore) CreateActiveConfig(_ context.Context, input CreateConfigInput) (LanguageConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	routes, err := normalizeOutputRoutes(input.LanguagePairs, input.OutputRoutes)
	if err != nil {
		return LanguageConfig{}, err
	}

	if input.IdempotencyKey != "" {
		if _, exists := s.idempo[input.IdempotencyKey]; exists {
			return LanguageConfig{}, ErrIdempotencyConflict
		}
	}

	currentIdx := -1
	nextVersion := 1
	for i := range s.configs {
		if s.configs[i].SessionID == input.SessionID && s.configs[i].Status == StatusActive {
			currentIdx = i
			nextVersion = s.configs[i].Version + 1
			break
		}
	}

	if input.ExpectedVersion != nil {
		if currentIdx < 0 || s.configs[currentIdx].Version != *input.ExpectedVersion {
			return LanguageConfig{}, ErrVersionConflict
		}
	}

	now := s.clock.Now().UTC()
	cfg := LanguageConfig{
		ID:                 ulid.Make().String(),
		SessionID:          input.SessionID,
		Version:            nextVersion,
		LanguagePairs:      append([]LanguagePair(nil), input.LanguagePairs...),
		OutputRoutes:       append([]OutputRoute(nil), routes...),
		Status:             StatusActive,
		EffectiveFrom:      now,
		CreatedBy:          input.CreatedBy,
		CreatedAt:          now,
		RequestFingerprint: input.RequestFingerprint,
	}
	event, err := languageConfigChangedEvent(cfg, input.TraceID)
	if err != nil {
		return LanguageConfig{}, err
	}
	if s.failEvent != nil {
		err := s.failEvent
		s.failEvent = nil
		return LanguageConfig{}, err
	}
	if currentIdx >= 0 {
		until := now
		s.configs[currentIdx].Status = StatusSuperseded
		s.configs[currentIdx].EffectiveUntil = &until
	}
	s.configs = append(s.configs, cfg)
	if input.IdempotencyKey != "" {
		s.idempo[input.IdempotencyKey] = cfg.ID
	}
	s.outbox = append(s.outbox, cloneLanguageConfigChangedEvent(event))
	return cloneConfig(cfg), nil
}

// LanguageConfigChangeEvents returns immutable copies for offline tests and
// local adapters. Production publishers use PostgresStore's claim methods.
func (s *MemoryStore) LanguageConfigChangeEvents() []realtimev1.LanguageConfigChangedEvent {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	events := make([]realtimev1.LanguageConfigChangedEvent, len(s.outbox))
	for i, event := range s.outbox {
		events[i] = cloneLanguageConfigChangedEvent(event)
	}
	return events
}

// FailNextLanguageConfigOutbox makes the next config creation fail before it
// changes active state, exercising the same all-or-nothing contract as the
// PostgreSQL transaction.
func (s *MemoryStore) FailNextLanguageConfigOutbox(err error) {
	if s == nil || err == nil {
		return
	}
	s.mu.Lock()
	s.failEvent = err
	s.mu.Unlock()
}

func (s *MemoryStore) ListConfigs(_ context.Context, query ListConfigsQuery) ([]LanguageConfig, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	var cursorVersion int
	hasCursor := false
	if query.Cursor != "" {
		v, err := strconv.Atoi(query.Cursor)
		if err != nil {
			return nil, "", fmt.Errorf("%w: invalid cursor", ErrInvalidRequest)
		}
		cursorVersion = v
		hasCursor = true
	}

	matched := make([]LanguageConfig, 0)
	for i := len(s.configs) - 1; i >= 0; i-- {
		cfg := s.configs[i]
		if cfg.SessionID != query.SessionID {
			continue
		}
		if hasCursor && cfg.Version >= cursorVersion {
			continue
		}
		matched = append(matched, cloneConfig(cfg))
	}

	var next string
	if len(matched) > limit {
		matched = matched[:limit]
		next = strconv.Itoa(matched[len(matched)-1].Version)
	}
	return matched, next, nil
}

func cloneConfig(cfg LanguageConfig) LanguageConfig {
	out := cfg
	out.LanguagePairs = append([]LanguagePair(nil), cfg.LanguagePairs...)
	out.OutputRoutes = append([]OutputRoute(nil), cfg.OutputRoutes...)
	out.OutputMode = outputModeForRoutes(out.OutputRoutes)
	if cfg.EffectiveUntil != nil {
		t := *cfg.EffectiveUntil
		out.EffectiveUntil = &t
	}
	return out
}

var _ Store = (*MemoryStore)(nil)
