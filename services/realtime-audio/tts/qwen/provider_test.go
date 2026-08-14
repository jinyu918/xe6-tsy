package qwen

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/1024XEngineer/xe6-tsy/services/realtime-audio/audio"
	"github.com/1024XEngineer/xe6-tsy/services/realtime-audio/tts"
	"github.com/gorilla/websocket"
)

func TestNewProviderRequiresCanonicalSampleRate(t *testing.T) {
	_, err := NewProvider(Config{APIKey: "test-key", BaseURL: "https://example.com", SampleRate: 16_000})
	if err == nil {
		t.Fatal("NewProvider() error = nil, want error for noncanonical sample rate")
	}

	provider, err := NewProvider(Config{APIKey: "test-key", BaseURL: "https://example.com"})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	if provider.config.SampleRate != audio.TTSSampleRate {
		t.Fatalf("SampleRate = %d, want %d", provider.config.SampleRate, audio.TTSSampleRate)
	}
}

func TestProviderStreamsQwenRealtimeAudio(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		if r.URL.Query().Get("model") != "qwen3-tts-flash-realtime" {
			t.Errorf("model = %q", r.URL.Query().Get("model"))
		}
		eventIDs := make(map[string]struct{})
		readEvent := func(label string) map[string]any {
			_, data, err := conn.ReadMessage()
			if err != nil {
				t.Errorf("read %s: %v", label, err)
				return nil
			}
			var event map[string]any
			if err := json.Unmarshal(data, &event); err != nil {
				t.Errorf("decode %s: %v", label, err)
				return nil
			}
			id, ok := event["event_id"].(string)
			if !ok || id == "" {
				t.Errorf("%s event_id = %#v", label, event["event_id"])
			} else if _, exists := eventIDs[id]; exists {
				t.Errorf("duplicate event_id %q", id)
			} else {
				eventIDs[id] = struct{}{}
			}
			return event
		}
		update := readEvent("session update")
		session, _ := update["session"].(map[string]any)
		if update["type"] != "session.update" || session["voice"] != "Cherry" || session["language_type"] != "Auto" || session["sample_rate"] != float64(realtimeSampleRate) {
			t.Errorf("update = %#v", update)
		}
		for i := 0; i < 2; i++ {
			readEvent("text event")
		}
		for _, audio := range [][]byte{{1, 2}, {3, 4}} {
			payload := map[string]any{"type": "response.audio.delta", "delta": base64.StdEncoding.EncodeToString(audio)}
			encoded, _ := json.Marshal(payload)
			_ = conn.WriteMessage(websocket.TextMessage, encoded)
		}
		done, _ := json.Marshal(map[string]any{"type": "response.done"})
		_ = conn.WriteMessage(websocket.TextMessage, done)
		readEvent("session finish")
		if len(eventIDs) != 4 {
			t.Errorf("event IDs = %d, want 4", len(eventIDs))
		}
		finished, _ := json.Marshal(map[string]any{"type": "session.finished"})
		_ = conn.WriteMessage(websocket.TextMessage, finished)
	}))
	defer server.Close()

	provider, err := NewProvider(Config{APIKey: "test-key", BaseURL: "ws" + strings.TrimPrefix(server.URL, "http"), Model: "qwen3-tts-flash-realtime", SampleRate: realtimeSampleRate})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	stream, err := provider.StartStream(context.Background(), tts.Request{Text: "hello", TargetLanguage: "fr-FR"})
	if err != nil {
		t.Fatalf("StartStream() error = %v", err)
	}
	var chunks []tts.AudioChunk
	for chunk := range stream.Chunks() {
		chunks = append(chunks, chunk)
	}
	result, err := stream.Finish(context.Background())
	if err != nil {
		t.Fatalf("Finish() error = %v", err)
	}
	if len(chunks) != 2 || string(chunks[1].Data) != string([]byte{3, 4}) {
		t.Fatalf("chunks = %#v", chunks)
	}
	if result.Model != "qwen3-tts-flash-realtime" || result.AudioDuration <= 0 {
		t.Fatalf("result = %#v", result)
	}
}

func TestProviderRealtimeDialUsesProviderTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	defer server.Close()

	provider, err := NewProvider(Config{
		APIKey:  "test-key",
		BaseURL: "ws" + strings.TrimPrefix(server.URL, "http"),
		Model:   realtimeModel,
		Timeout: 50 * time.Millisecond,
		Dialer:  &websocket.Dialer{},
	})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}

	result := make(chan error, 1)
	go func() {
		_, startErr := provider.StartStream(context.Background(), tts.Request{Text: "hello"})
		result <- startErr
	}()
	select {
	case err = <-result:
		if err == nil {
			t.Fatal("StartStream() unexpectedly connected")
		}
	case <-time.After(time.Second):
		t.Fatal("StartStream() exceeded the test deadline")
	}
}

func TestProviderStreamsQwenTTSAudio(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/services/aigc/multimodal-generation/generation" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" || r.Header.Get("X-DashScope-SSE") != "enable" {
			t.Errorf("headers = %#v", r.Header)
		}
		var request struct {
			Model string `json:"model"`
			Input struct {
				Text         string `json:"text"`
				Voice        string `json:"voice"`
				LanguageType string `json:"language_type"`
			} `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if request.Model != "qwen3-tts-flash" || request.Input.Text != "hello" || request.Input.Voice != "Cherry" || request.Input.LanguageType != "English" {
			t.Errorf("request = %#v", request)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: " + ttsEvent([]byte{1, 2}) + "\n\n"))
		_, _ = w.Write([]byte("data: " + ttsEvent([]byte{3, 4}) + "\n\n"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}))
	defer server.Close()

	provider, err := NewProvider(Config{APIKey: "test-key", BaseURL: server.URL + "/api/v1"})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	stream, err := provider.StartStream(context.Background(), tts.Request{Text: "hello", TargetLanguage: "en-US"})
	if err != nil {
		t.Fatalf("StartStream() error = %v", err)
	}
	var chunks []tts.AudioChunk
	for chunk := range stream.Chunks() {
		chunks = append(chunks, chunk)
	}
	result, err := stream.Finish(context.Background())
	if err != nil {
		t.Fatalf("Finish() error = %v", err)
	}
	if len(chunks) != 1 || chunks[0].SequenceNo != 1 || string(chunks[0].Data) != string([]byte{1, 2, 3, 4}) {
		t.Fatalf("chunks = %#v", chunks)
	}
	if result.Provider != "aliyun" || result.Model != "qwen3-tts-flash" {
		t.Fatalf("result = %#v", result)
	}
}

func TestProviderNormalizesWAVSSEAudio(t *testing.T) {
	wav := makeWAV16(t, 8000, 2, []int16{1000, -1000, 3000, 1000})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: " + ttsEvent(wav) + "\n\n"))
	}))
	defer server.Close()

	provider, err := NewProvider(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	stream, err := provider.StartStream(context.Background(), tts.Request{Text: "hello", TargetLanguage: "en-US"})
	if err != nil {
		t.Fatalf("StartStream() error = %v", err)
	}
	var chunks []tts.AudioChunk
	for chunk := range stream.Chunks() {
		chunks = append(chunks, chunk)
	}
	result, err := stream.Finish(context.Background())
	if err != nil {
		t.Fatalf("Finish() error = %v", err)
	}
	if len(chunks) != 1 || chunks[0].Encoding != audio.PCMEncoding || chunks[0].SampleRate != audio.TTSSampleRate || chunks[0].Channels != audio.MonoChannels {
		t.Fatalf("chunks = %#v", chunks)
	}
	if err := chunks[0].ValidateCanonicalPCM(); err != nil {
		t.Fatalf("normalized chunk is invalid: %v", err)
	}
	if len(chunks[0].Data) != 12 || result.AudioDuration <= 0 {
		t.Fatalf("normalized data/result = %d/%#v", len(chunks[0].Data), result)
	}
}

func TestProviderNormalizesWAVSSEAudioWhenFormatIsUndeclared(t *testing.T) {
	wav := makeWAV16(t, 8000, 2, []int16{1000, -1000, 3000, 1000})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: " + ttsEvent(wav) + "\n\n"))
	}))
	defer server.Close()

	provider, err := NewProvider(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	stream, err := provider.StartStream(context.Background(), tts.Request{Text: "hello", TargetLanguage: "en-US"})
	if err != nil {
		t.Fatalf("StartStream() error = %v", err)
	}
	var chunks []tts.AudioChunk
	for chunk := range stream.Chunks() {
		chunks = append(chunks, chunk)
	}
	if _, err := stream.Finish(context.Background()); err != nil {
		t.Fatalf("Finish() error = %v", err)
	}
	if len(chunks) != 1 || len(chunks[0].Data) != 12 {
		t.Fatalf("chunks = %#v", chunks)
	}
	if err := chunks[0].ValidateCanonicalPCM(); err != nil {
		t.Fatalf("undeclared WAV chunk is invalid: %v", err)
	}
}

func TestProviderNormalizesWAVAcrossUndeclaredSSEBoundaries(t *testing.T) {
	wav := makeWAV16(t, 8000, 2, []int16{1000, -1000, 3000, 1000})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, piece := range [][]byte{wav[:5], wav[5:11], wav[11:]} {
			_, _ = w.Write([]byte("data: " + ttsEvent(piece) + "\n\n"))
		}
	}))
	defer server.Close()

	provider, err := NewProvider(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	stream, err := provider.StartStream(context.Background(), tts.Request{Text: "hello", TargetLanguage: "en-US"})
	if err != nil {
		t.Fatalf("StartStream() error = %v", err)
	}
	var chunks []tts.AudioChunk
	for chunk := range stream.Chunks() {
		chunks = append(chunks, chunk)
	}
	if _, err := stream.Finish(context.Background()); err != nil {
		t.Fatalf("Finish() error = %v", err)
	}
	if len(chunks) != 1 || len(chunks[0].Data) != 12 {
		t.Fatalf("chunks = %#v", chunks)
	}
	if err := chunks[0].ValidateCanonicalPCM(); err != nil {
		t.Fatalf("split WAV chunk is invalid: %v", err)
	}
}

func TestProviderUsesMetadataSentBeforeAudio(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: " + ttsEventWithMetadata(nil, map[string]any{"encoding": "pcm_s16le", "sample_rate": 16000, "channels": 2}) + "\n\n"))
		_, _ = w.Write([]byte("data: " + ttsEvent([]byte{0xe8, 0x03, 0x18, 0xfc}) + "\n\n"))
	}))
	defer server.Close()

	provider, err := NewProvider(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	stream, err := provider.StartStream(context.Background(), tts.Request{Text: "hello", TargetLanguage: "en-US"})
	if err != nil {
		t.Fatalf("StartStream() error = %v", err)
	}
	var chunks []tts.AudioChunk
	for chunk := range stream.Chunks() {
		chunks = append(chunks, chunk)
	}
	if _, err := stream.Finish(context.Background()); err != nil {
		t.Fatalf("Finish() error = %v", err)
	}
	if len(chunks) != 1 || len(chunks[0].Data) == 0 {
		t.Fatalf("chunks = %#v", chunks)
	}
	if err := chunks[0].ValidateCanonicalPCM(); err != nil {
		t.Fatalf("metadata-first chunk is invalid: %v", err)
	}
}

func TestProviderUsesMetadataSentAfterShortPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: " + ttsEvent([]byte{0xe8, 0x03, 0x18, 0xfc}) + "\n\n"))
		_, _ = w.Write([]byte("data: " + ttsEventWithMetadata(nil, map[string]any{"encoding": "pcm_s16le", "sample_rate": 16000, "channels": 2}) + "\n\n"))
	}))
	defer server.Close()

	provider, err := NewProvider(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	stream, err := provider.StartStream(context.Background(), tts.Request{Text: "hello", TargetLanguage: "en-US"})
	if err != nil {
		t.Fatalf("StartStream() error = %v", err)
	}
	var chunks []tts.AudioChunk
	for chunk := range stream.Chunks() {
		chunks = append(chunks, chunk)
	}
	if _, err := stream.Finish(context.Background()); err != nil {
		t.Fatalf("Finish() error = %v", err)
	}
	if len(chunks) != 1 || len(chunks[0].Data) != 4 {
		t.Fatalf("chunks = %#v", chunks)
	}
	if err := chunks[0].ValidateCanonicalPCM(); err != nil {
		t.Fatalf("metadata-after-payload chunk is invalid: %v", err)
	}
}

func TestProviderNormalizesDeclaredStereoRawAcrossChunkBoundaries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: " + ttsEventWithMetadata([]byte{0xe8}, map[string]any{"encoding": "pcm_s16le", "sample_rate": 16000, "channels": 2}) + "\n\n"))
		_, _ = w.Write([]byte("data: " + ttsEventWithMetadata([]byte{0x03, 0x18, 0xfc}, nil) + "\n\n"))
	}))
	defer server.Close()

	provider, err := NewProvider(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	stream, err := provider.StartStream(context.Background(), tts.Request{Text: "hello", TargetLanguage: "en-US"})
	if err != nil {
		t.Fatalf("StartStream() error = %v", err)
	}
	var data []byte
	for chunk := range stream.Chunks() {
		if err := chunk.ValidateCanonicalPCM(); err != nil {
			t.Fatalf("chunk invalid: %v", err)
		}
		data = append(data, chunk.Data...)
	}
	if _, err := stream.Finish(context.Background()); err != nil {
		t.Fatalf("Finish() error = %v", err)
	}
	if len(data) == 0 || len(data)%2 != 0 {
		t.Fatalf("normalized data length = %d", len(data))
	}
}

func TestProviderRejectsUnsupportedContainer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: " + ttsEventWithMetadata([]byte{1, 2, 3, 4}, map[string]any{"encoding": "audio/mpeg"}) + "\n\n"))
	}))
	defer server.Close()

	provider, err := NewProvider(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	stream, err := provider.StartStream(context.Background(), tts.Request{Text: "hello", TargetLanguage: "en-US"})
	if err != nil {
		t.Fatalf("StartStream() error = %v", err)
	}
	for range stream.Chunks() {
	}
	if _, err := stream.Finish(context.Background()); !errors.Is(err, audio.ErrAudioEncoding) {
		t.Fatalf("Finish() error = %v, want audio.ErrAudioEncoding", err)
	}
}

func TestProviderRejectsNativeOpusAtAdapterBoundary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: " + ttsEventWithMetadata([]byte{1, 2, 3, 4}, map[string]any{"encoding": "audio/opus"}) + "\n\n"))
	}))
	defer server.Close()

	provider, err := NewProvider(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	stream, err := provider.StartStream(context.Background(), tts.Request{Text: "hello", TargetLanguage: "en-US"})
	if err != nil {
		t.Fatalf("StartStream() error = %v", err)
	}
	for range stream.Chunks() {
	}
	if _, err := stream.Finish(context.Background()); !errors.Is(err, audio.ErrAudioEncoding) {
		t.Fatalf("Finish() error = %v, want audio.ErrAudioEncoding", err)
	}
}

func TestProviderNormalizesURLAudioFromContentType(t *testing.T) {
	wav := makeWAV16(t, 16000, 1, []int16{1000, -1000})
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/audio" {
			w.Header().Set("Content-Type", "audio/wav")
			_, _ = w.Write(wav)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: " + ttsEventURL(server.URL+"/audio") + "\n\n"))
	}))
	defer server.Close()

	provider, err := NewProvider(Config{APIKey: "test-key", BaseURL: server.URL, AudioURLAllowlist: []string{"127.0.0.1"}})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	stream, err := provider.StartStream(context.Background(), tts.Request{Text: "hello", TargetLanguage: "en-US"})
	if err != nil {
		t.Fatalf("StartStream() error = %v", err)
	}
	for chunk := range stream.Chunks() {
		if err := chunk.ValidateCanonicalPCM(); err != nil {
			t.Fatalf("URL chunk invalid: %v", err)
		}
	}
	if _, err := stream.Finish(context.Background()); err != nil {
		t.Fatalf("Finish() error = %v", err)
	}
}

func TestProviderNormalizesURLRawPCMFromContentTypeParameters(t *testing.T) {
	raw := []byte{0xe8, 0x03, 0x18, 0xfc}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/audio" {
			w.Header().Set("Content-Type", "audio/pcm; rate=16000; channels=2")
			_, _ = w.Write(raw)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: " + ttsEventURL(server.URL+"/audio") + "\n\n"))
	}))
	defer server.Close()

	provider, err := NewProvider(Config{APIKey: "test-key", BaseURL: server.URL, AudioURLAllowlist: []string{"127.0.0.1"}})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	stream, err := provider.StartStream(context.Background(), tts.Request{Text: "hello", TargetLanguage: "en-US"})
	if err != nil {
		t.Fatalf("StartStream() error = %v", err)
	}
	var chunks []tts.AudioChunk
	for chunk := range stream.Chunks() {
		chunks = append(chunks, chunk)
	}
	if _, err := stream.Finish(context.Background()); err != nil {
		t.Fatalf("Finish() error = %v", err)
	}
	if len(chunks) != 1 || len(chunks[0].Data) == 0 {
		t.Fatalf("chunks = %#v", chunks)
	}
	if err := chunks[0].ValidateCanonicalPCM(); err != nil {
		t.Fatalf("URL PCM chunk is invalid: %v", err)
	}
}

func TestProviderRejectsChangingMetadataAfterCanonicalStreamingStarts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: " + ttsEvent(make([]byte, 12)) + "\n\n"))
		_, _ = w.Write([]byte("data: " + ttsEventWithMetadata([]byte{3, 4}, map[string]any{"sample_rate": 16000}) + "\n\n"))
	}))
	defer server.Close()

	provider, err := NewProvider(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	stream, err := provider.StartStream(context.Background(), tts.Request{Text: "hello", TargetLanguage: "en-US"})
	if err != nil {
		t.Fatalf("StartStream() error = %v", err)
	}
	for range stream.Chunks() {
	}
	if _, err := stream.Finish(context.Background()); !errors.Is(err, audio.ErrAudioFormat) {
		t.Fatalf("Finish() error = %v, want audio.ErrAudioFormat", err)
	}
}

func TestCosyVoiceRequestUsesMultilingualInstruction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/services/audio/tts/SpeechSynthesizer" {
			t.Errorf("path = %q", r.URL.Path)
		}
		var request struct {
			Model string `json:"model"`
			Input struct {
				Voice       string `json:"voice"`
				Instruction string `json:"instruction"`
			} `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if request.Model != "cosyvoice-v3.5-flash" || request.Input.Voice != "longanhuan_v3" || request.Input.Instruction != "请用日语自然地朗读。" {
			t.Errorf("request = %#v", request)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: " + ttsEvent([]byte{1, 2}) + "\n\n"))
	}))
	defer server.Close()

	provider, err := NewProvider(Config{
		APIKey: "test-key", BaseURL: server.URL + "/api/v1",
		Model: "cosyvoice-v3.5-flash", Voice: "longanhuan_v3",
	})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	stream, err := provider.StartStream(context.Background(), tts.Request{Text: "hello", TargetLanguage: "ja-JP"})
	if err != nil {
		t.Fatalf("StartStream() error = %v", err)
	}
	for range stream.Chunks() {
	}
	if _, err := stream.Finish(context.Background()); err != nil {
		t.Fatalf("Finish() error = %v", err)
	}
}

func TestFinishPrefersCompletedResultOverCanceledContext(t *testing.T) {
	done := make(chan struct{})
	close(done)
	want := tts.Result{Provider: "aliyun", Model: "qwen3-tts-flash"}
	stream := &stream{done: done, result: want}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := stream.Finish(ctx)
	if err != nil {
		t.Fatalf("Finish() error = %v", err)
	}
	if got != want {
		t.Fatalf("Finish() result = %#v, want %#v", got, want)
	}
}

func TestFinishCancellationDoesNotCloseChunksWhileWorkerSends(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for index := 0; index < 64; index++ {
			_, _ = w.Write([]byte("data: " + ttsEvent([]byte{byte(index)}) + "\n\n"))
		}
	}))
	defer server.Close()

	provider, err := NewProvider(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	providerStream, err := provider.StartStream(context.Background(), tts.Request{Text: "hello", TargetLanguage: "en-US"})
	if err != nil {
		t.Fatalf("StartStream() error = %v", err)
	}
	qwenStream := providerStream.(*stream)
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for len(qwenStream.chunks) < cap(qwenStream.chunks) {
		select {
		case <-deadline.C:
			t.Fatal("TTS worker did not fill the chunk buffer")
		default:
			runtime.Gosched()
		}
	}

	finishCtx, cancelFinish := context.WithCancel(context.Background())
	cancelFinish()
	if _, err := providerStream.Finish(finishCtx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Finish() error = %v, want context.Canceled", err)
	}
	if err := providerStream.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	for range providerStream.Chunks() {
	}
}

func TestProviderRejectsEmptyAudioResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
	}))
	defer server.Close()

	provider, err := NewProvider(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	stream, err := provider.StartStream(context.Background(), tts.Request{Text: "hello", TargetLanguage: "en-US"})
	if err != nil {
		t.Fatalf("StartStream() error = %v", err)
	}
	if _, err := stream.Finish(context.Background()); !errors.Is(err, ErrNoAudio) {
		t.Fatalf("Finish() error = %v, want ErrNoAudio", err)
	}
}

func TestDownloadAudioRequiresAllowlistedHost(t *testing.T) {
	stream := &stream{ctx: context.Background(), config: Config{HTTPClient: http.DefaultClient}}
	if _, err := stream.downloadAudio("http://127.0.0.1:12345/audio"); !errors.Is(err, ErrAudioURLNotAllowed) {
		t.Fatalf("downloadAudio() error = %v, want ErrAudioURLNotAllowed", err)
	}
}

func ttsEvent(audio []byte) string {
	return ttsEventWithMetadata(audio, nil)
}

func ttsEventWithMetadata(audio []byte, metadata map[string]any) string {
	data, _ := json.Marshal(generationResponse{Output: struct {
		Audio generationAudio `json:"audio"`
	}{}})
	var event map[string]any
	_ = json.Unmarshal(data, &event)
	audioValue := map[string]any{"data": base64.StdEncoding.EncodeToString(audio)}
	for key, value := range metadata {
		audioValue[key] = value
	}
	event["output"] = map[string]any{"audio": audioValue}
	data, _ = json.Marshal(event)
	return string(data)
}

func ttsEventURL(rawURL string) string {
	data, _ := json.Marshal(generationResponse{Output: struct {
		Audio generationAudio `json:"audio"`
	}{}})
	var event map[string]any
	_ = json.Unmarshal(data, &event)
	event["output"] = map[string]any{"audio": map[string]any{"url": rawURL}}
	data, _ = json.Marshal(event)
	return string(data)
}

func makeWAV16(t *testing.T, sampleRate, channels int, samples []int16) []byte {
	t.Helper()
	data := make([]byte, len(samples)*2)
	for index, sample := range samples {
		binary.LittleEndian.PutUint16(data[index*2:], uint16(sample))
	}
	result := make([]byte, 44+len(data))
	copy(result, []byte("RIFF"))
	binary.LittleEndian.PutUint32(result[4:], uint32(len(result)-8))
	copy(result[8:], []byte("WAVEfmt "))
	binary.LittleEndian.PutUint32(result[16:], 16)
	binary.LittleEndian.PutUint16(result[20:], 1)
	binary.LittleEndian.PutUint16(result[22:], uint16(channels))
	binary.LittleEndian.PutUint32(result[24:], uint32(sampleRate))
	binary.LittleEndian.PutUint32(result[28:], uint32(sampleRate*channels*2))
	binary.LittleEndian.PutUint16(result[32:], uint16(channels*2))
	binary.LittleEndian.PutUint16(result[34:], 16)
	copy(result[36:], []byte("data"))
	binary.LittleEndian.PutUint32(result[40:], uint32(len(data)))
	copy(result[44:], data)
	return result
}
