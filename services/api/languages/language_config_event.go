package languages

import (
	"fmt"
	"strings"

	realtimev1 "github.com/1024XEngineer/xe6-tsy/packages/contracts/realtime/v1"
)

// languageConfigChangedEvent converts one committed configuration into the
// immutable control-plane fact that downstream runtimes consume.
func languageConfigChangedEvent(config LanguageConfig, traceID string) (realtimev1.LanguageConfigChangedEvent, error) {
	traceID = strings.TrimSpace(traceID)
	if traceID == "" {
		traceID = "language-config:" + config.ID
	}
	event := realtimev1.LanguageConfigChangedEvent{
		EventVersion:          realtimev1.LanguageConfigChangedEventVersion,
		EventID:               "language-config:" + config.ID,
		TraceID:               traceID,
		SessionID:             config.SessionID,
		LanguageConfigVersion: int64(config.Version),
		OccurredAt:            config.CreatedAt.UTC(),
		LanguagePairs:         make([]realtimev1.LanguageConfigPair, len(config.LanguagePairs)),
		OutputRoutes:          make([]realtimev1.LanguageConfigOutputRoute, len(config.OutputRoutes)),
	}
	for i, pair := range config.LanguagePairs {
		event.LanguagePairs[i] = realtimev1.LanguageConfigPair{Source: pair.Source, Target: pair.Target}
	}
	for i, route := range config.OutputRoutes {
		event.OutputRoutes[i] = realtimev1.LanguageConfigOutputRoute{
			TargetLanguage:  route.TargetLanguage,
			TTSEnabled:      route.TTSEnabled,
			DeliveryEnabled: route.DeliveryEnabled,
		}
	}
	if err := event.Validate(); err != nil {
		return realtimev1.LanguageConfigChangedEvent{}, fmt.Errorf("validate language config change event: %w", err)
	}
	return event, nil
}

func cloneLanguageConfigChangedEvent(event realtimev1.LanguageConfigChangedEvent) realtimev1.LanguageConfigChangedEvent {
	clone := event
	clone.LanguagePairs = append([]realtimev1.LanguageConfigPair(nil), event.LanguagePairs...)
	clone.OutputRoutes = append([]realtimev1.LanguageConfigOutputRoute(nil), event.OutputRoutes...)
	return clone
}
