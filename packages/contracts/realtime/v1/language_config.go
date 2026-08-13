package realtimev1

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	// LanguageConfigChangedTopic carries the authoritative active language
	// configuration after the API transaction that created it commits.
	LanguageConfigChangedTopic = "language.config.changed"
	// LanguageConfigChangedEventVersion is the only schema version accepted by
	// v1 realtime consumers.
	LanguageConfigChangedEventVersion = 1
)

var ErrInvalidLanguageConfigChangedEvent = errors.New("invalid language config changed event")

// LanguageConfigPair is one explicit source-to-target translation direction
// captured in a language-configuration change event.
type LanguageConfigPair struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

// LanguageConfigOutputRoute carries media and delivery choices for one target
// language. It contains no provider selection or secret material.
type LanguageConfigOutputRoute struct {
	TargetLanguage  string `json:"target_language"`
	TTSEnabled      bool   `json:"tts_enabled"`
	DeliveryEnabled bool   `json:"delivery_enabled"`
}

// LanguageConfigChangedEvent is the durable control-plane fact consumed by
// realtime runtimes. EventID is its idempotency key; a consumer must retain
// the highest language configuration version per session and never promote an
// older event over a newer binding.
type LanguageConfigChangedEvent struct {
	EventVersion          int                         `json:"event_version"`
	EventID               string                      `json:"event_id"`
	TraceID               string                      `json:"trace_id"`
	SessionID             string                      `json:"session_id"`
	LanguageConfigVersion int64                       `json:"language_config_version"`
	LanguagePairs         []LanguageConfigPair        `json:"language_pairs"`
	OutputRoutes          []LanguageConfigOutputRoute `json:"output_routes"`
	OccurredAt            time.Time                   `json:"occurred_at"`
}

// Validate enforces the v1 payload boundary before an event enters an outbox
// or is accepted from a stream. It validates identifiers and route shape but
// deliberately leaves BCP-47 normalization to the language-config domain.
func (event LanguageConfigChangedEvent) Validate() error {
	switch {
	case event.EventVersion != LanguageConfigChangedEventVersion:
		return invalidLanguageConfigChangedField("event_version")
	case strings.TrimSpace(event.EventID) == "":
		return invalidLanguageConfigChangedField("event_id")
	case strings.TrimSpace(event.TraceID) == "":
		return invalidLanguageConfigChangedField("trace_id")
	case strings.TrimSpace(event.SessionID) == "":
		return invalidLanguageConfigChangedField("session_id")
	case event.LanguageConfigVersion < 1:
		return invalidLanguageConfigChangedField("language_config_version")
	case len(event.LanguagePairs) == 0:
		return invalidLanguageConfigChangedField("language_pairs")
	case len(event.OutputRoutes) == 0:
		return invalidLanguageConfigChangedField("output_routes")
	case event.OccurredAt.IsZero():
		return invalidLanguageConfigChangedField("occurred_at")
	}

	pairs := make(map[string]struct{}, len(event.LanguagePairs))
	for _, pair := range event.LanguagePairs {
		source := strings.TrimSpace(pair.Source)
		target := strings.TrimSpace(pair.Target)
		if source == "" || target == "" || source == target {
			return invalidLanguageConfigChangedField("language_pairs")
		}
		key := source + "\x00" + target
		if _, duplicate := pairs[key]; duplicate {
			return invalidLanguageConfigChangedField("language_pairs")
		}
		pairs[key] = struct{}{}
	}

	routes := make(map[string]struct{}, len(event.OutputRoutes))
	for _, route := range event.OutputRoutes {
		target := strings.TrimSpace(route.TargetLanguage)
		if target == "" {
			return invalidLanguageConfigChangedField("output_routes")
		}
		if _, duplicate := routes[target]; duplicate {
			return invalidLanguageConfigChangedField("output_routes")
		}
		routes[target] = struct{}{}
	}
	return nil
}

func invalidLanguageConfigChangedField(field string) error {
	return fmt.Errorf("%w: %s", ErrInvalidLanguageConfigChangedEvent, field)
}
