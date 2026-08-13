package localruntime

import (
	"context"
	"errors"
	"fmt"

	languagesv1 "github.com/1024XEngineer/xe6-tsy/packages/contracts/languages/v1"
	"github.com/jackc/pgx/v5"
)

var ErrSpeechCatalogReaderRequired = errors.New("postgres speech catalog reader is required")

type speechCatalogQueryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// PostgresSpeechCatalogLoader reads the immutable routing catalog used to
// construct the realtime process registry. It reads only enabled, unretired
// records; BuildSpeechRegistry remains responsible for validating the complete
// cross-table media contract before adapters are constructed.
type PostgresSpeechCatalogLoader struct {
	queryer speechCatalogQueryer
}

// NewPostgresSpeechCatalogLoader creates a loader from a pgx pool or
// transaction-compatible queryer. The narrow dependency keeps catalog mapping
// tests fully offline.
func NewPostgresSpeechCatalogLoader(queryer speechCatalogQueryer) *PostgresSpeechCatalogLoader {
	return &PostgresSpeechCatalogLoader{queryer: queryer}
}

// LoadSpeechCatalog returns the active profile and route snapshot for process
// startup. It does not fall back to environment-selected profiles when the
// database catalog is enabled.
func (r *PostgresSpeechCatalogLoader) LoadSpeechCatalog(ctx context.Context) (SpeechCatalog, error) {
	if err := ctx.Err(); err != nil {
		return SpeechCatalog{}, err
	}
	if r == nil || r.queryer == nil {
		return SpeechCatalog{}, ErrSpeechCatalogReaderRequired
	}

	asrProfiles, err := r.loadASRProfiles(ctx)
	if err != nil {
		return SpeechCatalog{}, err
	}
	ttsProfiles, err := r.loadTTSProfiles(ctx)
	if err != nil {
		return SpeechCatalog{}, err
	}
	routes, err := r.loadRoutes(ctx)
	if err != nil {
		return SpeechCatalog{}, err
	}
	return SpeechCatalog{ASRProfiles: asrProfiles, TTSProfiles: ttsProfiles, Routes: routes}, nil
}

func (r *PostgresSpeechCatalogLoader) loadASRProfiles(ctx context.Context) ([]languagesv1.ASRProfile, error) {
	rows, err := r.queryer.Query(ctx, activeASRProfilesSQL)
	if err != nil {
		return nil, fmt.Errorf("query active ASR speech profiles: %w", err)
	}
	defer rows.Close()

	profiles := make([]languagesv1.ASRProfile, 0)
	for rows.Next() {
		var profile languagesv1.ASRProfile
		if err := rows.Scan(
			&profile.ID,
			&profile.ProviderCode,
			&profile.Model,
			&profile.SupportedLanguages,
			&profile.SupportsAutoDetect,
			&profile.SupportsStreaming,
			&profile.InputEncoding,
			&profile.InputSampleRateHz,
			&profile.InputChannels,
			&profile.Enabled,
			&profile.RetiredAt,
			&profile.CreatedAt,
			&profile.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan active ASR speech profile: %w", err)
		}
		profiles = append(profiles, profile)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read active ASR speech profiles: %w", err)
	}
	return profiles, nil
}

func (r *PostgresSpeechCatalogLoader) loadTTSProfiles(ctx context.Context) ([]languagesv1.TTSProfile, error) {
	rows, err := r.queryer.Query(ctx, activeTTSProfilesSQL)
	if err != nil {
		return nil, fmt.Errorf("query active TTS speech profiles: %w", err)
	}
	defer rows.Close()

	profiles := make([]languagesv1.TTSProfile, 0)
	for rows.Next() {
		var profile languagesv1.TTSProfile
		if err := rows.Scan(
			&profile.ID,
			&profile.ProviderCode,
			&profile.Model,
			&profile.VoiceID,
			&profile.SupportedLanguages,
			&profile.SupportsStreaming,
			&profile.OutputEncoding,
			&profile.OutputSampleRateHz,
			&profile.OutputChannels,
			&profile.Enabled,
			&profile.RetiredAt,
			&profile.CreatedAt,
			&profile.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan active TTS speech profile: %w", err)
		}
		profiles = append(profiles, profile)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read active TTS speech profiles: %w", err)
	}
	return profiles, nil
}

func (r *PostgresSpeechCatalogLoader) loadRoutes(ctx context.Context) ([]languagesv1.SpeechRoute, error) {
	rows, err := r.queryer.Query(ctx, activeSpeechRoutesSQL)
	if err != nil {
		return nil, fmt.Errorf("query active speech routes: %w", err)
	}
	defer rows.Close()

	routes := make([]languagesv1.SpeechRoute, 0)
	for rows.Next() {
		var route languagesv1.SpeechRoute
		if err := rows.Scan(
			&route.ID,
			&route.LanguageA,
			&route.LanguageB,
			&route.ASRProfileID,
			&route.TTSProfileID,
			&route.Enabled,
			&route.RetiredAt,
		); err != nil {
			return nil, fmt.Errorf("scan active speech route: %w", err)
		}
		routes = append(routes, route)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read active speech routes: %w", err)
	}
	return routes, nil
}

const activeASRProfilesSQL = `
SELECT
    profile.id,
    profile.provider_code,
    profile.model,
    ARRAY(
        SELECT language_code
        FROM speech_asr_profile_languages
        WHERE profile_id = profile.id
        ORDER BY language_code
    ),
    profile.supports_auto_detect,
    profile.supports_streaming,
    profile.input_encoding,
    profile.input_sample_rate_hz,
    profile.input_channels,
    profile.enabled,
    profile.retired_at,
    profile.created_at,
    profile.updated_at
FROM speech_asr_profiles AS profile
WHERE profile.enabled = TRUE
  AND profile.retired_at IS NULL
ORDER BY profile.id`

const activeTTSProfilesSQL = `
SELECT
    profile.id,
    profile.provider_code,
    profile.model,
    profile.voice_id,
    ARRAY(
        SELECT language_code
        FROM speech_tts_profile_languages
        WHERE profile_id = profile.id
        ORDER BY language_code
    ),
    profile.supports_streaming,
    profile.output_encoding,
    profile.output_sample_rate_hz,
    profile.output_channels,
    profile.enabled,
    profile.retired_at,
    profile.created_at,
    profile.updated_at
FROM speech_tts_profiles AS profile
WHERE profile.enabled = TRUE
  AND profile.retired_at IS NULL
ORDER BY profile.id`

const activeSpeechRoutesSQL = `
SELECT
    route.id,
    route.language_a,
    route.language_b,
    route.asr_profile_id,
    route.tts_profile_id,
    route.enabled,
    route.retired_at
FROM speech_language_pair_routes AS route
WHERE route.enabled = TRUE
  AND route.retired_at IS NULL
ORDER BY route.language_a, route.language_b, route.id`
