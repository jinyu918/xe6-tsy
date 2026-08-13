package qwen

import (
	"fmt"
	"mime"
	"strconv"
	"strings"

	"github.com/1024XEngineer/xe6-tsy/services/realtime-audio/audio"
)

type audioMetadata struct {
	encoding   string
	sampleRate int
	channels   int
}

type downloadedAudio struct {
	data     []byte
	metadata audioMetadata
}

func (m audioMetadata) merge(other audioMetadata) audioMetadata {
	if encoding := normalizeEncoding(other.encoding); encoding != "" {
		m.encoding = encoding
	}
	if other.sampleRate > 0 {
		m.sampleRate = other.sampleRate
	}
	if other.channels > 0 {
		m.channels = other.channels
	}
	return m
}

func (m audioMetadata) empty() bool {
	return strings.TrimSpace(m.encoding) == "" && m.sampleRate == 0 && m.channels == 0
}

func mergeAudioMetadata(current, next audioMetadata) (audioMetadata, error) {
	currentEncoding := normalizeEncoding(current.encoding)
	nextEncoding := normalizeEncoding(next.encoding)
	if currentEncoding != "" && nextEncoding != "" && currentEncoding != nextEncoding {
		return audioMetadata{}, fmt.Errorf("%w: audio encoding changed from %q to %q", audio.ErrAudioFormat, currentEncoding, nextEncoding)
	}
	if current.sampleRate > 0 && next.sampleRate > 0 && current.sampleRate != next.sampleRate {
		return audioMetadata{}, fmt.Errorf("%w: audio sample rate changed from %d to %d", audio.ErrAudioFormat, current.sampleRate, next.sampleRate)
	}
	if current.channels > 0 && next.channels > 0 && current.channels != next.channels {
		return audioMetadata{}, fmt.Errorf("%w: audio channels changed from %d to %d", audio.ErrAudioFormat, current.channels, next.channels)
	}
	return current.merge(next), nil
}

func (m audioMetadata) normalized() audioMetadata {
	m.encoding = normalizeEncoding(m.encoding)
	if m.encoding == "" {
		m.encoding = audio.PCMEncoding
	}
	if m.sampleRate == 0 {
		m.sampleRate = audio.TTSSampleRate
	}
	if m.channels == 0 {
		m.channels = audio.MonoChannels
	}
	return m
}

func normalizeEncoding(encoding string) string {
	encoding = strings.ToLower(strings.TrimSpace(encoding))
	switch encoding {
	case "pcm", "raw", "audio/pcm", "audio/x-pcm":
		return audio.PCMEncoding
	case "wave", "wav", "audio/wav", "audio/x-wav":
		return "wav"
	case "opus", "audio/opus", "audio/ogg":
		return "opus"
	default:
		return encoding
	}
}

func contentTypeMetadata(contentType string) audioMetadata {
	mediaType, parameters, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0])
	}
	return audioMetadata{
		encoding:   normalizeEncoding(mediaType),
		sampleRate: contentTypeInt(parameters, "sample_rate", "samplerate", "rate"),
		channels:   contentTypeInt(parameters, "channels", "channel_count"),
	}
}

func contentTypeInt(parameters map[string]string, keys ...string) int {
	for _, key := range keys {
		value, err := strconv.Atoi(strings.TrimSpace(parameters[key]))
		if err == nil && value > 0 {
			return value
		}
	}
	return 0
}

func normalizeAudio(data []byte, metadata audioMetadata) ([]byte, error) {
	metadata.encoding = normalizeEncoding(metadata.encoding)
	if isWAVPayload(data) {
		switch metadata.encoding {
		case "", "wav":
			metadata.encoding = "wav"
		case audio.PCMEncoding:
			return nil, fmt.Errorf("%w: declared PCM contains a RIFF/WAVE header", audio.ErrAudioFormat)
		}
	}
	metadata = metadata.normalized()
	pcm, err := audio.NormalizePCM(data, metadata.encoding, metadata.sampleRate, metadata.channels, audio.TTSSampleRate, audio.MonoChannels)
	if err != nil {
		return nil, err
	}
	return pcm.Data, nil
}

func isWAVPayload(data []byte) bool {
	return len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WAVE"
}

func isCanonicalRaw(metadata audioMetadata, first []byte) bool {
	metadata = metadata.normalized()
	if metadata.encoding != audio.PCMEncoding {
		return false
	}
	if isWAVPayload(first) {
		return false
	}
	return metadata.sampleRate == audio.TTSSampleRate && metadata.channels == audio.MonoChannels
}

func matchesCanonicalMetadata(metadata audioMetadata) bool {
	encoding := normalizeEncoding(metadata.encoding)
	return (encoding == "" || encoding == audio.PCMEncoding) &&
		(metadata.sampleRate == 0 || metadata.sampleRate == audio.TTSSampleRate) &&
		(metadata.channels == 0 || metadata.channels == audio.MonoChannels)
}

func canDetermineAudioFormat(metadata audioMetadata, data []byte) bool {
	return normalizeEncoding(metadata.encoding) != "" || metadata.sampleRate > 0 || metadata.channels > 0 || len(data) >= 12
}

func (a generationAudio) metadata() audioMetadata {
	return audioMetadata{
		encoding:   firstNonEmpty(a.Encoding, a.Format, a.AudioFormat),
		sampleRate: firstPositive(a.SampleRate, a.SampleRateHz),
		channels:   firstPositive(a.Channels, a.ChannelCount),
	}
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}
