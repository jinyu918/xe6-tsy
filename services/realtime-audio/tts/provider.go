package tts

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/1024XEngineer/xe6-tsy/services/realtime-audio/audio"
)

var ErrAudioChunkInvalid = errors.New("invalid TTS audio chunk")

// Request identifies the Turn and playback produced from translated text.
type Request struct {
	SessionID      string
	TurnID         string
	PlaybackID     string
	Text           string
	TargetLanguage string
	VoiceID        string
}

// AudioChunk is one ordered unit of synthesized audio.
type AudioChunk struct {
	SequenceNo int64
	Encoding   string
	SampleRate int
	Channels   int
	Data       []byte
}

// ValidateCanonicalPCM enforces the provider-to-pipeline media invariant.
// Adapters must decode vendor containers before exposing a chunk here.
func (c AudioChunk) ValidateCanonicalPCM() error {
	if c.Encoding != audio.PCMEncoding {
		return fmt.Errorf("%w: encoding %q", ErrAudioChunkInvalid, c.Encoding)
	}
	if c.SampleRate != audio.TTSSampleRate {
		return fmt.Errorf("%w: sample rate %d", ErrAudioChunkInvalid, c.SampleRate)
	}
	if c.Channels != audio.MonoChannels {
		return fmt.Errorf("%w: channels %d", ErrAudioChunkInvalid, c.Channels)
	}
	if err := audio.ValidatePCM(c.Data, c.SampleRate, c.Channels); err != nil {
		return fmt.Errorf("%w: %w", ErrAudioChunkInvalid, err)
	}
	return nil
}

// Result contains synthesis provider and usage metadata.
type Result struct {
	Provider      string
	Model         string
	AudioDuration time.Duration
	CostAmount    string
	Currency      string
}

// Provider starts one synthesis stream.
type Provider interface {
	StartStream(ctx context.Context, request Request) (Stream, error)
}

// Stream exposes synthesized audio and its completed usage result.
type Stream interface {
	Chunks() <-chan AudioChunk
	Finish(ctx context.Context) (Result, error)
	Close() error
}
