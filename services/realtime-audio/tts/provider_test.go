package tts

import (
	"errors"
	"testing"

	"github.com/1024XEngineer/xe6-tsy/services/realtime-audio/audio"
)

func TestAudioChunkValidateCanonicalPCM(t *testing.T) {
	tests := []struct {
		name  string
		chunk AudioChunk
		want  error
	}{
		{
			name: "valid",
			chunk: AudioChunk{Encoding: audio.PCMEncoding, SampleRate: audio.TTSSampleRate,
				Channels: audio.MonoChannels, Data: []byte{1, 0}},
		},
		{name: "container", chunk: AudioChunk{Encoding: "audio/wav", SampleRate: audio.TTSSampleRate, Channels: 1, Data: []byte{1, 0}}, want: ErrAudioChunkInvalid},
		{name: "wrong rate", chunk: AudioChunk{Encoding: audio.PCMEncoding, SampleRate: 16000, Channels: 1, Data: []byte{1, 0}}, want: ErrAudioChunkInvalid},
		{name: "wrong channels", chunk: AudioChunk{Encoding: audio.PCMEncoding, SampleRate: audio.TTSSampleRate, Channels: 2, Data: []byte{1, 0, 2, 0}}, want: ErrAudioChunkInvalid},
		{name: "odd PCM", chunk: AudioChunk{Encoding: audio.PCMEncoding, SampleRate: audio.TTSSampleRate, Channels: 1, Data: []byte{1}}, want: ErrAudioChunkInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.chunk.ValidateCanonicalPCM()
			if !errors.Is(err, tt.want) {
				t.Fatalf("ValidateCanonicalPCM() error = %v, want %v", err, tt.want)
			}
		})
	}
}
