package audio

import (
	"encoding/binary"
	"errors"
	"testing"
)

func TestNormalizePCMConvertsWAVStereoToTTSMono(t *testing.T) {
	input := makeWAV16(t, 8000, 2, []int16{1000, -1000, 3000, 1000})
	got, err := NormalizePCM(input, "", 0, 0, TTSSampleRate, MonoChannels)
	if err != nil {
		t.Fatalf("NormalizePCM() error = %v", err)
	}
	if got.SampleRate != TTSSampleRate || got.Channels != MonoChannels {
		t.Fatalf("format = %#v", got)
	}
	if len(got.Data) != 12 {
		t.Fatalf("normalized bytes = %d, want 12", len(got.Data))
	}
	if first := int16(binary.LittleEndian.Uint16(got.Data)); first != 0 {
		t.Fatalf("first downmixed sample = %d, want 0", first)
	}
}

func TestNormalizePCMIsDeterministic(t *testing.T) {
	input := make([]byte, 6)
	negative := int16(-1000)
	binary.LittleEndian.PutUint16(input[0:], uint16(negative))
	binary.LittleEndian.PutUint16(input[2:], uint16(int16(0)))
	binary.LittleEndian.PutUint16(input[4:], uint16(int16(1000)))
	one, err := NormalizePCM(input, PCMEncoding, 16000, 1, TTSSampleRate, 1)
	if err != nil {
		t.Fatalf("first normalization error = %v", err)
	}
	two, err := NormalizePCM(input, PCMEncoding, 16000, 1, TTSSampleRate, 1)
	if err != nil {
		t.Fatalf("second normalization error = %v", err)
	}
	if string(one.Data) != string(two.Data) {
		t.Fatal("same input produced different PCM")
	}
}

func TestNormalizePCMRejectsUnsupportedAndMalformedInput(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		enc  string
		want error
	}{
		{name: "unknown encoding", data: []byte{0, 0}, enc: "mp3", want: ErrAudioEncoding},
		{name: "odd PCM", data: []byte{0}, enc: PCMEncoding, want: ErrPCMAlignment},
		{name: "bad wav", data: []byte("RIFF"), enc: "wav", want: ErrAudioFormat},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NormalizePCM(test.data, test.enc, 24000, 1, 24000, 1)
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func makeWAV16(t *testing.T, sampleRate, channels int, samples []int16) []byte {
	t.Helper()
	data := make([]byte, len(samples)*2)
	for i, sample := range samples {
		binary.LittleEndian.PutUint16(data[i*2:], uint16(sample))
	}
	buf := make([]byte, 44+len(data))
	copy(buf, []byte("RIFF"))
	binary.LittleEndian.PutUint32(buf[4:], uint32(len(buf)-8))
	copy(buf[8:], []byte("WAVEfmt "))
	binary.LittleEndian.PutUint32(buf[16:], 16)
	binary.LittleEndian.PutUint16(buf[20:], 1)
	binary.LittleEndian.PutUint16(buf[22:], uint16(channels))
	binary.LittleEndian.PutUint32(buf[24:], uint32(sampleRate))
	binary.LittleEndian.PutUint32(buf[28:], uint32(sampleRate*channels*2))
	binary.LittleEndian.PutUint16(buf[32:], uint16(channels*2))
	binary.LittleEndian.PutUint16(buf[34:], 16)
	copy(buf[36:], []byte("data"))
	binary.LittleEndian.PutUint32(buf[40:], uint32(len(data)))
	copy(buf[44:], data)
	return buf
}
