package webrtc

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/1024XEngineer/xe6-tsy/services/realtime-audio/audio"
	"github.com/1024XEngineer/xe6-tsy/services/realtime-audio/playback"
	"github.com/1024XEngineer/xe6-tsy/services/realtime-audio/segment"
	"github.com/pion/opus"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

const (
	defaultTTSSampleRate = audio.TTSSampleRate
	defaultASRSampleRate = audio.SupportedSampleRate
	defaultMediaChannels = audio.MonoChannels
)

var (
	ErrMediaUnavailable    = errors.New("WebRTC media is unavailable")
	ErrMediaConfigInvalid  = errors.New("WebRTC media configuration is invalid")
	ErrRemoteTrackRequired = errors.New("remote audio track is required")
	ErrRemoteTrackAttached = errors.New("remote audio track is already attached")
	ErrDecoderRequired     = errors.New("RTP audio decoder is required")
	ErrPlaybackStopped     = errors.New("playback track is stopped")
)

// MediaConfig defines canonical TTS input and outbound WebRTC media behavior.
// TTS chunks are always pcm_s16le/24kHz/mono; an attached send track is always
// encoded as Opus/48kHz/mono. Inbound Opus is decoded separately at the ASR
// pipeline's fixed sample rate.
type MediaConfig struct {
	TTSTrackID       string
	DataChannelLabel string
	SampleRate       int
	Channels         int
	// SkipTTSTrack disables TTS track attachment and the matching remote-offer
	// codec check. The PCM DataChannel mode uses this path.
	SkipTTSTrack bool
	// DownlinkCodec is "opus" when a WebRTC TTS track is attached; "pcm" and
	// "none" are accepted only with SkipTTSTrack for non-track output modes.
	DownlinkCodec string
}

func (c MediaConfig) normalized() (MediaConfig, error) {
	if c.TTSTrackID == "" {
		c.TTSTrackID = defaultTTSTrackID
	}
	if c.DataChannelLabel == "" {
		c.DataChannelLabel = defaultDataChannelLabel
	}
	if c.SampleRate == 0 {
		c.SampleRate = defaultTTSSampleRate
	}
	if c.Channels == 0 {
		c.Channels = defaultMediaChannels
	}
	if c.SampleRate != audio.TTSSampleRate || c.Channels != audio.MonoChannels {
		return MediaConfig{}, ErrMediaConfigInvalid
	}
	c.DownlinkCodec = strings.ToLower(strings.TrimSpace(c.DownlinkCodec))
	if c.SkipTTSTrack {
		switch c.DownlinkCodec {
		case "", "none", "pcm":
			return c, nil
		default:
			return MediaConfig{}, ErrMediaConfigInvalid
		}
	}
	if c.DownlinkCodec == "" {
		c.DownlinkCodec = "opus"
	}
	if c.DownlinkCodec != "opus" {
		return MediaConfig{}, ErrMediaConfigInvalid
	}
	return c, nil
}

// MediaTransport exposes the optional media capabilities of a signaling transport.
// ConnectionManager continues to depend only on ConnectionTransport.
type MediaTransport interface {
	ConnectionTransport
	AudioSource() segment.FrameSource
	TTSAudioTrack() playback.AudioTrack
	TranslationEvents() *PionEventSink
	Playback() *playback.Service
}

// PionEventSink serializes playback events to one translation-events DataChannel.
type PionEventSink struct {
	channel  pionDataChannel
	open     chan struct{}
	closed   chan struct{}
	openOne  sync.Once
	closeOne sync.Once
	stateMu  sync.Mutex
	closeErr error
	sendMu   sync.Mutex
}

type pionDataChannel interface {
	OnOpen(func())
	ReadyState() webrtc.DataChannelState
	SendText(string) error
}

func newPionEventSink(channel pionDataChannel) *PionEventSink {
	sink := &PionEventSink{channel: channel, open: make(chan struct{}), closed: make(chan struct{})}
	if channel != nil {
		channel.OnOpen(func() { sink.openOne.Do(func() { close(sink.open) }) })
		if channel.ReadyState() == webrtc.DataChannelStateOpen {
			sink.openOne.Do(func() { close(sink.open) })
		}
	}
	return sink
}

func (s *PionEventSink) close(err error) {
	if s == nil {
		return
	}
	if err == nil {
		err = ErrTransportClosed
	}
	s.closeOne.Do(func() {
		s.stateMu.Lock()
		s.closeErr = err
		s.stateMu.Unlock()
		close(s.closed)
	})
}

func (s *PionEventSink) terminalError() error {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.closeErr != nil {
		return s.closeErr
	}
	return ErrTransportClosed
}

// Publish waits for DataChannel open and sends one JSON event in order.
func (s *PionEventSink) Publish(ctx context.Context, event playback.Event) error {
	return s.PublishJSON(ctx, event)
}

// PublishJSON waits for DataChannel open and sends one arbitrary JSON value.
func (s *PionEventSink) PublishJSON(ctx context.Context, value any) error {
	if s == nil || s.channel == nil {
		return ErrMediaUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.open:
	case <-s.closed:
		return s.terminalError()
	}
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	select {
	case <-s.closed:
		return s.terminalError()
	default:
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode translation event: %w", err)
	}
	if err := s.channel.SendText(string(payload)); err != nil {
		return fmt.Errorf("send translation event: %w", err)
	}
	return nil
}

// RTPDecoder converts one RTP payload to normalized PCM.
type RTPDecoder interface {
	Decode(payload []byte) ([]byte, error)
}

// OpusDecoder is the default pure-Go decoder for browser WebRTC audio.
type OpusDecoder struct {
	decoder opus.Decoder
	output  []int16
}

// NewOpusDecoder creates a mono decoder at the normalized pipeline sample rate.
func NewOpusDecoder() (*OpusDecoder, error) {
	decoder, err := opus.NewDecoderWithOutput(defaultASRSampleRate, defaultMediaChannels)
	if err != nil {
		return nil, err
	}
	return &OpusDecoder{decoder: decoder, output: make([]int16, defaultASRSampleRate*120/1000)}, nil
}

// Decode returns copied signed 16-bit little-endian PCM.
func (d *OpusDecoder) Decode(payload []byte) ([]byte, error) {
	if d == nil {
		return nil, ErrDecoderRequired
	}
	if len(payload) == 0 {
		return nil, ErrDecoderRequired
	}
	samples, err := d.decoder.DecodeToInt16(payload, d.output)
	if err != nil {
		return nil, fmt.Errorf("decode Opus RTP payload: %w", err)
	}
	pcm := make([]byte, samples*2)
	for index, sample := range d.output[:samples] {
		binary.LittleEndian.PutUint16(pcm[index*2:], uint16(sample))
	}
	return pcm, nil
}

// PionAudioSource turns one remote Opus track into normalized audio.Frame values.
type PionAudioSource struct {
	decoder RTPDecoder
	now     func() time.Time
	frames  chan audio.Frame
	done    chan struct{}
	close   sync.Once

	mu       sync.Mutex
	err      error
	attached bool
}

type pionRemoteTrack interface {
	ReadRTP() (*rtp.Packet, error)
}

func newPionAudioSource(decoder RTPDecoder, now func() time.Time) (*PionAudioSource, error) {
	if decoder == nil {
		return nil, ErrDecoderRequired
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &PionAudioSource{decoder: decoder, now: now, frames: make(chan audio.Frame, 16), done: make(chan struct{})}, nil
}

// Attach starts the reader for the remote track exactly once.
func (s *PionAudioSource) Attach(track pionRemoteTrack) error {
	if s == nil || track == nil {
		return ErrRemoteTrackRequired
	}
	s.mu.Lock()
	if s.attached {
		s.mu.Unlock()
		return ErrRemoteTrackAttached
	}
	s.attached = true
	s.mu.Unlock()
	go s.readLoop(track)
	return nil
}

func (s *PionAudioSource) readLoop(track pionRemoteTrack) {
	var lastCaptured time.Time
	for {
		packet, err := track.ReadRTP()
		if err != nil {
			if !errors.Is(err, io.EOF) {
				s.setError(err)
			}
			s.closeDone()
			return
		}
		pcm, err := s.decoder.Decode(packet.Payload)
		if err != nil || len(pcm) == 0 {
			// Browser Opus comfort-noise / FEC / partial packets must not kill
			// the whole realtime session; skip and keep reading.
			continue
		}
		capturedAt := s.now()
		if !lastCaptured.IsZero() && !capturedAt.After(lastCaptured) {
			capturedAt = lastCaptured.Add(time.Millisecond)
		}
		lastCaptured = capturedAt
		frame, err := audio.NewFrame(pcm, defaultASRSampleRate, capturedAt)
		if err != nil {
			continue
		}
		select {
		case s.frames <- frame:
		case <-s.done:
			return
		}
	}
}

// ReadFrame returns decoded audio or the terminal track error.
func (s *PionAudioSource) ReadFrame(ctx context.Context) (audio.Frame, error) {
	if s == nil {
		return audio.Frame{}, ErrMediaUnavailable
	}
	select {
	case <-ctx.Done():
		return audio.Frame{}, ctx.Err()
	case frame := <-s.frames:
		return frame, nil
	case <-s.done:
		select {
		case frame := <-s.frames:
			return frame, nil
		default:
		}
		s.mu.Lock()
		err := s.err
		s.mu.Unlock()
		if err != nil {
			return audio.Frame{}, err
		}
		return audio.Frame{}, io.EOF
	}
}

// Close stops delivery and is idempotent; the owning PeerConnection closes the remote reader.
func (s *PionAudioSource) Close() error {
	if s == nil {
		return nil
	}
	s.closeDone()
	return nil
}

func (s *PionAudioSource) setError(err error) {
	s.mu.Lock()
	if s.err == nil {
		s.err = err
	}
	s.mu.Unlock()
}

func (s *PionAudioSource) closeDone() {
	s.close.Do(func() {
		close(s.done)
	})
}

var _ RTPDecoder = (*OpusDecoder)(nil)
var _ playback.EventSink = (*PionEventSink)(nil)
var _ interface {
	ReadFrame(context.Context) (audio.Frame, error)
	Close() error
} = (*PionAudioSource)(nil)
