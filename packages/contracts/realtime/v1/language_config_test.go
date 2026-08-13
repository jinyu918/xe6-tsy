package realtimev1

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestLanguageConfigChangedEventValidation(t *testing.T) {
	event := validLanguageConfigChangedEvent()
	if err := event.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*LanguageConfigChangedEvent)
	}{
		{name: "event version", mutate: func(event *LanguageConfigChangedEvent) { event.EventVersion = 2 }},
		{name: "event id", mutate: func(event *LanguageConfigChangedEvent) { event.EventID = "  " }},
		{name: "trace id", mutate: func(event *LanguageConfigChangedEvent) { event.TraceID = "" }},
		{name: "session id", mutate: func(event *LanguageConfigChangedEvent) { event.SessionID = "" }},
		{name: "configuration version", mutate: func(event *LanguageConfigChangedEvent) { event.LanguageConfigVersion = 0 }},
		{name: "no language pairs", mutate: func(event *LanguageConfigChangedEvent) { event.LanguagePairs = nil }},
		{name: "invalid language pair", mutate: func(event *LanguageConfigChangedEvent) { event.LanguagePairs[0].Source = event.LanguagePairs[0].Target }},
		{name: "duplicate language pair", mutate: func(event *LanguageConfigChangedEvent) {
			event.LanguagePairs = append(event.LanguagePairs, event.LanguagePairs[0])
		}},
		{name: "no output routes", mutate: func(event *LanguageConfigChangedEvent) { event.OutputRoutes = nil }},
		{name: "invalid output route", mutate: func(event *LanguageConfigChangedEvent) { event.OutputRoutes[0].TargetLanguage = "" }},
		{name: "duplicate output route", mutate: func(event *LanguageConfigChangedEvent) {
			event.OutputRoutes = append(event.OutputRoutes, event.OutputRoutes[0])
		}},
		{name: "occurred at", mutate: func(event *LanguageConfigChangedEvent) { event.OccurredAt = time.Time{} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			invalid := validLanguageConfigChangedEvent()
			test.mutate(&invalid)
			if err := invalid.Validate(); !errors.Is(err, ErrInvalidLanguageConfigChangedEvent) {
				t.Fatalf("Validate() error = %v, want ErrInvalidLanguageConfigChangedEvent", err)
			}
		})
	}
}

func TestLanguageConfigChangedEventJSONRoundTrip(t *testing.T) {
	want := validLanguageConfigChangedEvent()
	encoded, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var got LanguageConfigChangedEvent
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("round-trip Validate() error = %v", err)
	}
	if got.EventID != want.EventID || got.LanguageConfigVersion != want.LanguageConfigVersion ||
		len(got.LanguagePairs) != len(want.LanguagePairs) || len(got.OutputRoutes) != len(want.OutputRoutes) {
		t.Fatalf("round-trip event = %#v, want %#v", got, want)
	}
}

func TestAsyncAPILanguageConfigChangedMatchesGoContract(t *testing.T) {
	spec := readYAMLMap(t, filepath.Join("..", "..", "events.yaml"))
	channels := nestedMap(t, spec, "channels")
	if got := nestedMap(t, channels, "languageConfigChanged")["address"]; got != LanguageConfigChangedTopic {
		t.Fatalf("language config topic = %v, want %q", got, LanguageConfigChangedTopic)
	}

	schemas := nestedMap(t, nestedMap(t, spec, "components"), "schemas")
	schema := nestedMap(t, schemas, "LanguageConfigChangedEvent")
	assertStringList(t, schema["required"], []string{
		"event_version", "event_id", "trace_id", "session_id", "language_config_version",
		"language_pairs", "output_routes", "occurred_at",
	})
	properties := nestedMap(t, schema, "properties")
	if got := nestedMap(t, properties, "event_version")["const"]; got != LanguageConfigChangedEventVersion {
		t.Fatalf("event_version = %v, want %d", got, LanguageConfigChangedEventVersion)
	}
	if got := nestedMap(t, properties, "language_config_version")["minimum"]; got != 1 {
		t.Fatalf("language_config_version minimum = %v, want 1", got)
	}
}

func validLanguageConfigChangedEvent() LanguageConfigChangedEvent {
	return LanguageConfigChangedEvent{
		EventVersion:          LanguageConfigChangedEventVersion,
		EventID:               "language-config-1",
		TraceID:               "trace-1",
		SessionID:             "session-1",
		LanguageConfigVersion: 2,
		LanguagePairs: []LanguageConfigPair{
			{Source: "zh-CN", Target: "en-US"},
			{Source: "en-US", Target: "zh-CN"},
		},
		OutputRoutes: []LanguageConfigOutputRoute{
			{TargetLanguage: "en-US", TTSEnabled: true},
			{TargetLanguage: "zh-CN", DeliveryEnabled: true},
		},
		OccurredAt: time.Unix(1_700_000_000, 0).UTC(),
	}
}
