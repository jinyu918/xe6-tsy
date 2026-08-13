package localruntime

import (
	"context"
	"errors"
	"fmt"

	"github.com/1024XEngineer/xe6-tsy/services/realtime-audio/speech"
	"github.com/jackc/pgx/v5"
)

var ErrSpeechRouteReaderRequired = errors.New("postgres speech route reader is required")

type speechRouteQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// PostgresSpeechRouteResolver reads the active route for each binding prepare.
// Profile adapters remain process-scoped; only the language-pair selection is
// refreshed so a later session start or language switch sees current routing.
type PostgresSpeechRouteResolver struct {
	queryer speechRouteQueryer
}

// NewPostgresSpeechRouteResolver creates a resolver backed by a pgx pool or
// another QueryRow-compatible database handle.
func NewPostgresSpeechRouteResolver(queryer speechRouteQueryer) *PostgresSpeechRouteResolver {
	return &PostgresSpeechRouteResolver{queryer: queryer}
}

// ResolveBinding normalizes either language ordering, reads the current active
// route, and validates the returned route against the speech resolver contract.
func (r *PostgresSpeechRouteResolver) ResolveBinding(ctx context.Context, languageA, languageB string) (speech.SpeechRoute, error) {
	if err := ctx.Err(); err != nil {
		return speech.SpeechRoute{}, err
	}
	if r == nil || r.queryer == nil {
		return speech.SpeechRoute{}, ErrSpeechRouteReaderRequired
	}
	languageA, languageB, err := canonicalSpeechPair(languageA, languageB)
	if err != nil {
		return speech.SpeechRoute{}, err
	}

	var route speech.SpeechRoute
	err = r.queryer.QueryRow(ctx, activeSpeechRouteByPairSQL, languageA, languageB).Scan(
		&route.LanguageA,
		&route.LanguageB,
		&route.ASRProfileID,
		&route.TTSProfileID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return speech.SpeechRoute{}, fmt.Errorf("%w: %s and %s", speech.ErrSpeechRouteNotFound, languageA, languageB)
	}
	if err != nil {
		return speech.SpeechRoute{}, fmt.Errorf("load active speech route: %w", err)
	}

	validated, err := speech.NewRouteResolver([]speech.SpeechRoute{route})
	if err != nil {
		return speech.SpeechRoute{}, fmt.Errorf("validate active speech route: %w", err)
	}
	return validated.ResolveBinding(ctx, languageA, languageB)
}

const activeSpeechRouteByPairSQL = `
SELECT
    route.language_a,
    route.language_b,
    route.asr_profile_id,
    route.tts_profile_id
FROM speech_language_pair_routes AS route
WHERE route.language_a = $1
  AND route.language_b = $2
  AND route.enabled = TRUE
  AND route.retired_at IS NULL`

var _ speech.RouteResolver = (*PostgresSpeechRouteResolver)(nil)
