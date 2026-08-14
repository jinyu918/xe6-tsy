package audio

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
)

const (
	// PCMEncoding is the only audio representation exchanged by realtime
	// adapters and the pipeline.
	PCMEncoding = "pcm_s16le"
	// ASRSampleRate is the fixed input rate for the recognition contract.
	ASRSampleRate = 16_000
	// TTSSampleRate is the fixed output rate for the synthesis contract.
	TTSSampleRate = 24_000
	// OpusSampleRate is the rate advertised and encoded by the WebRTC downlink.
	OpusSampleRate = 48_000
	MonoChannels   = 1
)

var (
	ErrAudioEncoding = errors.New("unsupported audio encoding")
	ErrAudioFormat   = errors.New("invalid audio format")
)

// PCM describes signed 16-bit little-endian interleaved PCM at one sample rate.
// Data is always copied by NormalizePCM so a provider buffer cannot be mutated
// after it crosses the adapter boundary.
type PCM struct {
	Data       []byte
	SampleRate int
	Channels   int
}

// NormalizePCM decodes a raw PCM or RIFF/WAVE payload and converts it to the
// requested sample rate and channel count. It intentionally accepts only PCM16
// and Ogg-independent raw formats; vendor containers remain inside adapters.
func NormalizePCM(data []byte, encoding string, sampleRate, channels, targetRate, targetChannels int) (PCM, error) {
	if len(data) == 0 {
		return PCM{}, ErrPCMRequired
	}
	if targetRate <= 0 || targetChannels <= 0 {
		return PCM{}, fmt.Errorf("%w: invalid target %dHz/%d channels", ErrAudioFormat, targetRate, targetChannels)
	}
	encoding = strings.ToLower(strings.TrimSpace(encoding))
	if encoding == "" && isWAV(data) {
		encoding = "wav"
	}
	var err error
	switch encoding {
	case PCMEncoding, "pcm", "raw", "audio/pcm", "audio/x-pcm":
		if err = validatePCM(data, sampleRate, channels); err != nil {
			return PCM{}, err
		}
	case "wav", "wave", "audio/wav", "audio/x-wav":
		data, sampleRate, channels, err = decodeWAV(data)
		if err != nil {
			return PCM{}, err
		}
	default:
		return PCM{}, fmt.Errorf("%w: %q", ErrAudioEncoding, encoding)
	}
	if err := validatePCM(data, sampleRate, channels); err != nil {
		return PCM{}, err
	}
	data, err = convertChannels(data, channels, targetChannels)
	if err != nil {
		return PCM{}, err
	}
	if sampleRate != targetRate {
		data = resamplePCM(data, sampleRate, targetRate, targetChannels)
	}
	return PCM{Data: append([]byte(nil), data...), SampleRate: targetRate, Channels: targetChannels}, nil
}

// ValidatePCM checks the normalized signed 16-bit little-endian contract.
func ValidatePCM(data []byte, sampleRate, channels int) error {
	return validatePCM(data, sampleRate, channels)
}

func validatePCM(data []byte, sampleRate, channels int) error {
	if len(data) == 0 {
		return ErrPCMRequired
	}
	if len(data)%2 != 0 {
		return ErrPCMAlignment
	}
	if sampleRate <= 0 || channels <= 0 {
		return fmt.Errorf("%w: %dHz/%d channels", ErrAudioFormat, sampleRate, channels)
	}
	if len(data)%(2*channels) != 0 {
		return fmt.Errorf("%w: incomplete interleaved frame", ErrAudioFormat)
	}
	return nil
}

func isWAV(data []byte) bool {
	return len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WAVE"
}

func decodeWAV(data []byte) ([]byte, int, int, error) {
	if !isWAV(data) {
		return nil, 0, 0, fmt.Errorf("%w: missing RIFF/WAVE header", ErrAudioFormat)
	}
	var sampleRate, channels, bits, format int
	var pcm []byte
	for offset := 12; offset+8 <= len(data); {
		id := string(data[offset : offset+4])
		size := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		offset += 8
		if size < 0 || offset+size > len(data) {
			return nil, 0, 0, fmt.Errorf("%w: truncated %s chunk", ErrAudioFormat, id)
		}
		chunk := data[offset : offset+size]
		switch id {
		case "fmt ":
			if len(chunk) < 16 {
				return nil, 0, 0, fmt.Errorf("%w: short fmt chunk", ErrAudioFormat)
			}
			format = int(binary.LittleEndian.Uint16(chunk[0:2]))
			channels = int(binary.LittleEndian.Uint16(chunk[2:4]))
			sampleRate = int(binary.LittleEndian.Uint32(chunk[4:8]))
			bits = int(binary.LittleEndian.Uint16(chunk[14:16]))
		case "data":
			pcm = append([]byte(nil), chunk...)
		}
		offset += size
		if size%2 != 0 {
			offset++
		}
	}
	if format != 1 || bits != 16 || sampleRate <= 0 || channels <= 0 || pcm == nil {
		return nil, 0, 0, fmt.Errorf("%w: WAV must contain PCM16 data", ErrAudioFormat)
	}
	return pcm, sampleRate, channels, nil
}

func convertChannels(data []byte, sourceChannels, targetChannels int) ([]byte, error) {
	if sourceChannels == targetChannels {
		return append([]byte(nil), data...), nil
	}
	if targetChannels != MonoChannels || sourceChannels < 1 {
		return nil, fmt.Errorf("%w: %d to %d channels", ErrAudioFormat, sourceChannels, targetChannels)
	}
	samples := len(data) / 2 / sourceChannels
	out := make([]byte, samples*2)
	for i := 0; i < samples; i++ {
		var sum int64
		for channel := 0; channel < sourceChannels; channel++ {
			offset := (i*sourceChannels + channel) * 2
			sum += int64(int16(binary.LittleEndian.Uint16(data[offset:])))
		}
		value := sum / int64(sourceChannels)
		if value > math.MaxInt16 {
			value = math.MaxInt16
		}
		if value < math.MinInt16 {
			value = math.MinInt16
		}
		binary.LittleEndian.PutUint16(out[i*2:], uint16(int16(value)))
	}
	return out, nil
}

func resamplePCM(data []byte, sourceRate, targetRate, channels int) []byte {
	if sourceRate == targetRate {
		return append([]byte(nil), data...)
	}
	sourceSamples := len(data) / 2 / channels
	if sourceSamples == 0 {
		return nil
	}
	targetSamples := (sourceSamples*targetRate + sourceRate/2) / sourceRate
	if targetSamples < 1 {
		targetSamples = 1
	}
	out := make([]byte, targetSamples*channels*2)
	for target := 0; target < targetSamples; target++ {
		position := float64(target) * float64(sourceRate) / float64(targetRate)
		left := int(position)
		if left >= sourceSamples {
			left = sourceSamples - 1
		}
		right := left + 1
		if right >= sourceSamples {
			right = left
		}
		fraction := position - float64(left)
		for channel := 0; channel < channels; channel++ {
			leftOffset := (left*channels + channel) * 2
			rightOffset := (right*channels + channel) * 2
			leftSample := float64(int16(binary.LittleEndian.Uint16(data[leftOffset:])))
			rightSample := float64(int16(binary.LittleEndian.Uint16(data[rightOffset:])))
			value := int16(math.Round(leftSample + (rightSample-leftSample)*fraction))
			binary.LittleEndian.PutUint16(out[(target*channels+channel)*2:], uint16(value))
		}
	}
	return out
}
