package localruntime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/1024XEngineer/xe6-tsy/services/realtime-audio/audio"
	"github.com/1024XEngineer/xe6-tsy/services/realtime-audio/pipeline"
	"github.com/1024XEngineer/xe6-tsy/services/realtime-audio/playback"
	"github.com/1024XEngineer/xe6-tsy/services/realtime-audio/segment"
	"github.com/1024XEngineer/xe6-tsy/services/realtime-audio/tts"
	"github.com/1024XEngineer/xe6-tsy/services/realtime-audio/webrtc"
)

func TestSplitPCMBytes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		dataLen   int
		max       int
		wantCount int
		wantLast  int
	}{
		{name: "empty", dataLen: 0, max: 8, wantCount: 0},
		{name: "exact chunks", dataLen: maxTTSPCMChunkBytes * 2, max: maxTTSPCMChunkBytes, wantCount: 2, wantLast: maxTTSPCMChunkBytes},
		{name: "remainder", dataLen: maxTTSPCMChunkBytes*2 + 4, max: maxTTSPCMChunkBytes, wantCount: 3, wantLast: 4},
		{name: "nonpositive max uses default", dataLen: 6, max: 0, wantCount: 1, wantLast: 6},
		{name: "negative max uses default", dataLen: 6, max: -1, wantCount: 1, wantLast: 6},
		{name: "odd limit rounds down", dataLen: 6, max: 3, wantCount: 3, wantLast: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			pieces, err := splitPCMBytes(make([]byte, test.dataLen), test.max)
			if err != nil {
				t.Fatalf("splitPCMBytes() error = %v", err)
			}
			if len(pieces) != test.wantCount {
				t.Fatalf("len(pieces)=%d, want %d", len(pieces), test.wantCount)
			}
			if test.wantCount == 0 {
				return
			}
			if len(pieces[len(pieces)-1]) != test.wantLast {
				t.Fatalf("last=%d, want %d", len(pieces[len(pieces)-1]), test.wantLast)
			}
			for _, piece := range pieces {
				if len(piece)%2 != 0 {
					t.Fatalf("piece length = %d, want a complete PCM sample", len(piece))
				}
			}
		})
	}

	if _, err := splitPCMBytes([]byte{1}, 8); !errors.Is(err, audio.ErrPCMAlignment) {
		t.Fatalf("splitPCMBytes(odd PCM) error = %v, want %v", err, audio.ErrPCMAlignment)
	}
	if _, err := splitPCMBytes([]byte{1, 0}, 1); err == nil {
		t.Fatal("splitPCMBytes(limit smaller than one sample) error = nil")
	}
}

func TestDataChannelTTSAudioSinkPublishCompleteCancel(t *testing.T) {
	t.Parallel()

	t.Run("publish ignores empty and canceled", func(t *testing.T) {
		t.Parallel()
		sink := &DataChannelTTSAudioSink{}
		canceled, cancel := context.WithCancel(context.Background())
		cancel()
		if err := sink.Publish(canceled, canonicalTTSChunk("", "p1", []byte{1, 0})); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled Publish error = %v", err)
		}
		if err := sink.Publish(context.Background(), canonicalTTSChunk("", "", []byte{1, 0})); err != nil {
			t.Fatalf("empty playback Publish error = %v", err)
		}
		if err := sink.Publish(context.Background(), canonicalTTSChunk("", "p1", nil)); err != nil {
			t.Fatalf("empty data Publish error = %v", err)
		}
	})

	t.Run("complete with nil media accepts canonical chunks", func(t *testing.T) {
		t.Parallel()
		sink := &DataChannelTTSAudioSink{}
		pcm := make([]byte, maxTTSPCMChunkBytes+4)
		if err := sink.Publish(context.Background(), canonicalTTSChunk("session-1", "playback-1", pcm)); err != nil {
			t.Fatalf("Publish() error = %v", err)
		}
		if err := sink.Complete(context.Background(), "session-1", "playback-1"); err != nil {
			t.Fatalf("Complete() error = %v", err)
		}
	})

	t.Run("rejects noncanonical media", func(t *testing.T) {
		t.Parallel()
		tests := []struct {
			name  string
			chunk pipeline.AudioChunk
		}{
			{name: "container", chunk: pipeline.AudioChunk{PlaybackID: "p1", Encoding: "audio/wav", SampleRate: audio.TTSSampleRate, Channels: 1, Data: []byte{1, 0}}},
			{name: "wrong rate", chunk: pipeline.AudioChunk{PlaybackID: "p1", Encoding: audio.PCMEncoding, SampleRate: 16_000, Channels: 1, Data: []byte{1, 0}}},
			{name: "stereo", chunk: pipeline.AudioChunk{PlaybackID: "p1", Encoding: audio.PCMEncoding, SampleRate: audio.TTSSampleRate, Channels: 2, Data: []byte{1, 0, 2, 0}}},
			{name: "partial sample", chunk: pipeline.AudioChunk{PlaybackID: "p1", Encoding: audio.PCMEncoding, SampleRate: audio.TTSSampleRate, Channels: 1, Data: []byte{1}}},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				err := (&DataChannelTTSAudioSink{}).Publish(context.Background(), test.chunk)
				if !errors.Is(err, tts.ErrAudioChunkInvalid) {
					t.Fatalf("Publish() error = %v, want %v", err, tts.ErrAudioChunkInvalid)
				}
			})
		}
	})

	t.Run("complete canceled context", func(t *testing.T) {
		t.Parallel()
		sink := &DataChannelTTSAudioSink{}
		_ = sink.Publish(context.Background(), canonicalTTSChunk("s1", "p1", []byte{1, 0}))
		canceled, cancel := context.WithCancel(context.Background())
		cancel()
		if err := sink.Complete(canceled, "s1", "p1"); !errors.Is(err, context.Canceled) {
			t.Fatalf("Complete canceled = %v", err)
		}
	})

	t.Run("cancel drops buffer and surfaces context error", func(t *testing.T) {
		t.Parallel()
		sink := &DataChannelTTSAudioSink{}
		_ = sink.Publish(context.Background(), canonicalTTSChunk("s1", "p1", []byte{1, 0}))
		canceled, cancel := context.WithCancel(context.Background())
		cancel()
		if err := sink.Cancel(canceled, "s1", "p1", "interrupt"); !errors.Is(err, context.Canceled) {
			t.Fatalf("Cancel() error = %v", err)
		}
		if err := sink.Complete(context.Background(), "s1", "p1"); err != nil {
			t.Fatalf("Complete after cancel error = %v", err)
		}
	})

	t.Run("publish with nil translation events is best effort", func(t *testing.T) {
		t.Parallel()
		sink := &DataChannelTTSAudioSink{
			Media: mediaLookupFunc(func(context.Context, string) (webrtc.MediaTransport, error) {
				return &fakeMediaTransport{}, nil
			}),
		}
		if err := sink.Publish(context.Background(), canonicalTTSChunk("session-1", "playback-1", []byte{1, 0, 2, 0})); err != nil {
			t.Fatalf("Publish() error = %v", err)
		}
		if err := sink.Complete(context.Background(), "session-1", "playback-1"); err != nil {
			t.Fatalf("Complete with nil events error = %v", err)
		}
	})
}

func TestDataChannelTTSAudioSinkInterruptCurrentDropsSessionBuffers(t *testing.T) {
	sink := &DataChannelTTSAudioSink{}
	if err := sink.Publish(context.Background(), canonicalTTSChunk("session-1", "playback-1", []byte{1, 0})); err != nil {
		t.Fatalf("Publish(session-1) error = %v", err)
	}
	if err := sink.Publish(context.Background(), canonicalTTSChunk("session-2", "playback-2", []byte{2, 0})); err != nil {
		t.Fatalf("Publish(session-2) error = %v", err)
	}
	if err := sink.InterruptCurrent(context.Background(), "session-1", "wake_word_detected"); err != nil {
		t.Fatalf("InterruptCurrent() error = %v", err)
	}
	if len(sink.buffers) != 1 {
		t.Fatalf("buffers after interrupt = %d, want only another session", len(sink.buffers))
	}
	if _, ok := sink.buffers[ttsPlaybackKey{sessionID: "session-1", playbackID: "playback-1"}]; ok {
		t.Fatal("session-1 playback buffer was not removed")
	}
	if err := sink.Publish(context.Background(), canonicalTTSChunk("session-1", "playback-1", []byte{3, 0})); err != nil {
		t.Fatalf("Publish(late chunk) error = %v", err)
	}
	if _, ok := sink.buffers[ttsPlaybackKey{sessionID: "session-1", playbackID: "playback-1"}]; ok {
		t.Fatal("late chunk recreated an interrupted playback buffer")
	}
	if err := sink.Complete(context.Background(), "session-1", "playback-1"); err != nil {
		t.Fatalf("Complete(interrupted) error = %v", err)
	}
	if _, ok := sink.settled[ttsPlaybackKey{sessionID: "session-1", playbackID: "playback-1"}]; ok {
		t.Fatal("final completion did not release interrupted playback marker")
	}
}

func TestDataChannelTTSAudioSinkInterruptionIsScopedToSession(t *testing.T) {
	sink := &DataChannelTTSAudioSink{}
	if err := sink.Publish(context.Background(), canonicalTTSChunk("session-1", "shared-playback", []byte{1, 0})); err != nil {
		t.Fatalf("Publish(session-1) error = %v", err)
	}
	if err := sink.Publish(context.Background(), canonicalTTSChunk("session-2", "shared-playback", []byte{2, 0})); err != nil {
		t.Fatalf("Publish(session-2) error = %v", err)
	}
	if err := sink.InterruptCurrent(context.Background(), "session-1", "wake_word_detected"); err != nil {
		t.Fatalf("InterruptCurrent() error = %v", err)
	}
	buffer := sink.buffers[ttsPlaybackKey{sessionID: "session-2", playbackID: "shared-playback"}]
	if buffer == nil || string(buffer.pcm) != string([]byte{2, 0}) {
		t.Fatalf("another session buffer = %#v, want retained session-2 audio", buffer)
	}
}

func TestDataChannelTTSAudioSinkSeparatesSamePlaybackIDAcrossSessions(t *testing.T) {
	type publishedAudio struct {
		sessionID string
		data      []byte
	}
	var published []publishedAudio
	sink := &DataChannelTTSAudioSink{
		publishAudio: func(_ context.Context, sessionID, _, _ string, _ int64, _ bool, data []byte) error {
			published = append(published, publishedAudio{sessionID: sessionID, data: append([]byte(nil), data...)})
			return nil
		},
	}

	chunks := []pipeline.AudioChunk{
		canonicalTTSChunk("session-1", "shared-playback", []byte{1, 0}),
		canonicalTTSChunk("session-2", "shared-playback", []byte{3, 0}),
	}
	start := make(chan struct{})
	errs := make(chan error, len(chunks))
	var publishers sync.WaitGroup
	for _, chunk := range chunks {
		chunk := chunk
		publishers.Add(1)
		go func() {
			defer publishers.Done()
			<-start
			errs <- sink.Publish(context.Background(), chunk)
		}()
	}
	close(start)
	publishers.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Publish() error = %v", err)
		}
	}
	if len(sink.buffers) != 2 {
		t.Fatalf("buffers = %d, want one per session", len(sink.buffers))
	}
	for _, sessionID := range []string{"session-1", "session-2"} {
		if err := sink.Complete(context.Background(), sessionID, "shared-playback"); err != nil {
			t.Fatalf("Complete(%s) error = %v", sessionID, err)
		}
	}

	if len(published) != 2 {
		t.Fatalf("published audio count = %d, want 2", len(published))
	}
	if published[0].sessionID != "session-1" || string(published[0].data) != string([]byte{1, 0}) {
		t.Fatalf("first published audio = %#v, want session-1 data only", published[0])
	}
	if published[1].sessionID != "session-2" || string(published[1].data) != string([]byte{3, 0}) {
		t.Fatalf("second published audio = %#v, want session-2 data only", published[1])
	}
}

func TestDataChannelTTSAudioSinkCancelIsScopedToSessionWithSharedPlaybackID(t *testing.T) {
	sink := &DataChannelTTSAudioSink{}
	for _, chunk := range []pipeline.AudioChunk{
		canonicalTTSChunk("session-1", "shared-playback", []byte{1, 0}),
		canonicalTTSChunk("session-2", "shared-playback", []byte{2, 0}),
	} {
		if err := sink.Publish(context.Background(), chunk); err != nil {
			t.Fatalf("Publish(%s) error = %v", chunk.SessionID, err)
		}
	}
	if err := sink.Cancel(context.Background(), "session-1", "shared-playback", "interrupted"); err != nil {
		t.Fatalf("Cancel(session-1) error = %v", err)
	}

	if _, ok := sink.buffers[ttsPlaybackKey{sessionID: "session-1", playbackID: "shared-playback"}]; ok {
		t.Fatal("canceled session buffer was retained")
	}
	buffer := sink.buffers[ttsPlaybackKey{sessionID: "session-2", playbackID: "shared-playback"}]
	if buffer == nil || string(buffer.pcm) != string([]byte{2, 0}) {
		t.Fatalf("uncanceled session buffer = %#v, want session-2 audio", buffer)
	}
}

func TestDataChannelTTSAudioSinkInterruptCurrentStopsPublishingChunks(t *testing.T) {
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	published := make(chan int64, 2)
	sink := &DataChannelTTSAudioSink{
		publishAudio: func(_ context.Context, _, _, _ string, sequence int64, _ bool, _ []byte) error {
			published <- sequence
			if sequence == 1 {
				close(firstStarted)
				<-releaseFirst
			}
			return nil
		},
	}
	audioData := make([]byte, maxTTSPCMChunkBytes+2)
	if err := sink.Publish(context.Background(), canonicalTTSChunk("session-1", "playback-1", audioData)); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	complete := make(chan error, 1)
	go func() {
		complete <- sink.Complete(context.Background(), "session-1", "playback-1")
	}()
	<-firstStarted
	if err := sink.InterruptCurrent(context.Background(), "session-1", "wake_word_detected"); err != nil {
		t.Fatalf("InterruptCurrent() error = %v", err)
	}
	close(releaseFirst)
	if err := <-complete; err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	close(published)
	var sequences []int64
	for sequence := range published {
		sequences = append(sequences, sequence)
	}
	if len(sequences) != 1 || sequences[0] != 1 {
		t.Fatalf("published sequences = %v, want only in-flight first chunk", sequences)
	}
	key := ttsPlaybackKey{sessionID: "session-1", playbackID: "playback-1"}
	if _, ok := sink.publishing[key]; ok {
		t.Fatal("completed playback remained in publishing index")
	}
	if _, ok := sink.settled[key]; ok {
		t.Fatal("completed playback retained its interruption tombstone")
	}
}

func TestDataChannelTTSAudioSinkPublishingInterruptSurvivesTombstoneEviction(t *testing.T) {
	key := ttsPlaybackKey{sessionID: "session-1", playbackID: "playback-1"}
	sink := &DataChannelTTSAudioSink{
		publishing: map[ttsPlaybackKey]bool{key: true},
	}
	for index := 0; index <= maxSettledPlaybackTombstones; index++ {
		sink.mu.Lock()
		sink.markSettledLocked(ttsPlaybackKey{
			sessionID:  "another-session",
			playbackID: fmt.Sprintf("playback-%d", index),
		})
		sink.mu.Unlock()
	}
	if !sink.playbackSettled(key) {
		t.Fatal("publishing interruption was lost after tombstone eviction")
	}
}

func TestDataChannelTTSAudioSinkCountsUnavailableChannel(t *testing.T) {
	failures := &recordingDataChannelFailures{}
	sink := &DataChannelTTSAudioSink{Failures: failures}
	if err := sink.publish(t.Context(), "session-1", "playback-1", "turn-1", 1, true, []byte{1, 0}); err != nil {
		t.Fatalf("publish() error = %v", err)
	}
	if failures.calls != 1 {
		t.Fatalf("data channel failures = %d, want 1", failures.calls)
	}
}

func canonicalTTSChunk(sessionID, playbackID string, pcm []byte) pipeline.AudioChunk {
	return pipeline.AudioChunk{
		SessionID: sessionID, PlaybackID: playbackID,
		Encoding: audio.PCMEncoding, SampleRate: audio.TTSSampleRate, Channels: audio.MonoChannels,
		Data: pcm,
	}
}

type mediaLookupFunc func(ctx context.Context, sessionID string) (webrtc.MediaTransport, error)

func (f mediaLookupFunc) CurrentMedia(ctx context.Context, sessionID string) (webrtc.MediaTransport, error) {
	return f(ctx, sessionID)
}

type fakeMediaTransport struct {
	source segment.FrameSource
	events *webrtc.PionEventSink
}

func (f *fakeMediaTransport) Answer(context.Context, webrtc.SessionDescription) (webrtc.SessionDescription, error) {
	return webrtc.SessionDescription{}, nil
}
func (*fakeMediaTransport) AddCandidate(context.Context, webrtc.ICECandidate) error { return nil }
func (*fakeMediaTransport) EndCandidates(context.Context) error                     { return nil }
func (*fakeMediaTransport) Close(context.Context) error                             { return nil }
func (f *fakeMediaTransport) AudioSource() segment.FrameSource                      { return f.source }
func (*fakeMediaTransport) TTSAudioTrack() playback.AudioTrack                      { return nil }
func (f *fakeMediaTransport) TranslationEvents() *webrtc.PionEventSink              { return f.events }
func (*fakeMediaTransport) Playback() *playback.Service                             { return nil }
