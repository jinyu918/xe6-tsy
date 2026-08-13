package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	realtimev1 "github.com/1024XEngineer/xe6-tsy/packages/contracts/realtime/v1"
	recordsv1 "github.com/1024XEngineer/xe6-tsy/packages/contracts/records/v1"
	"github.com/1024XEngineer/xe6-tsy/services/realtime-audio/asr"
	"github.com/1024XEngineer/xe6-tsy/services/realtime-audio/assistant"
	"github.com/1024XEngineer/xe6-tsy/services/realtime-audio/config"
	"github.com/1024XEngineer/xe6-tsy/services/realtime-audio/controlchannel"
	"github.com/1024XEngineer/xe6-tsy/services/realtime-audio/controlplane"
	"github.com/1024XEngineer/xe6-tsy/services/realtime-audio/languageevents"
	"github.com/1024XEngineer/xe6-tsy/services/realtime-audio/localruntime"
	realtimemetrics "github.com/1024XEngineer/xe6-tsy/services/realtime-audio/metrics"
	"github.com/1024XEngineer/xe6-tsy/services/realtime-audio/pipeline"
	"github.com/1024XEngineer/xe6-tsy/services/realtime-audio/runtime"
	"github.com/1024XEngineer/xe6-tsy/services/realtime-audio/session"
	"github.com/1024XEngineer/xe6-tsy/services/realtime-audio/speech"
	"github.com/1024XEngineer/xe6-tsy/services/realtime-audio/translate"
	"github.com/1024XEngineer/xe6-tsy/services/realtime-audio/tts"
	"github.com/1024XEngineer/xe6-tsy/services/realtime-audio/vad"
	"github.com/1024XEngineer/xe6-tsy/services/realtime-audio/vad/silero"
	"github.com/1024XEngineer/xe6-tsy/services/realtime-audio/webrtc"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultAddr           = ":8090"
	minTicketSecretBytes  = 32
	httpReadHeaderTimeout = 5 * time.Second
	httpReadTimeout       = 30 * time.Second
	httpWriteTimeout      = 45 * time.Second
	httpIdleTimeout       = 60 * time.Second
)

type processConfig struct {
	Addr           string
	TicketSecret   string
	SkipTTSTrack   bool
	ForceMockTTS   bool
	DownlinkMode   string // none | pcm | opus
	DownlinkCodec  string
	SourceLanguage string
	TargetLanguage string
	MetricsToken   string
}

// controlPlaneRuntime groups the HTTP handler with resources that must outlive
// individual requests. Production startup owns this object so background
// language-config preparation stops before its Valkey, outbox, and database
// dependencies are released.
type controlPlaneRuntime struct {
	Handler   http.Handler
	resources *controlPlaneResources

	closeOnce sync.Once
	closeErr  error
}

type controlPlaneResources struct {
	languageRuntime interface{ Close() error }
	languageCancel  context.CancelFunc
	languageDone    <-chan struct{}
	outbox          interface{ Close() error }
	closePool       func()
}

// Close uses the required dependency order: consumer cancellation and pending
// binding settlement, consumer Valkey client, realtime outbox, then PostgreSQL.
func (r *controlPlaneRuntime) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.closeOnce.Do(func() {
		r.closeErr = r.resources.Close(ctx)
	})
	return r.closeErr
}

func (r *controlPlaneResources) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	var closeErr error
	if r.languageCancel != nil {
		r.languageCancel()
	}
	if r.languageDone != nil {
		select {
		case <-r.languageDone:
		case <-ctx.Done():
			closeErr = errors.Join(closeErr, ctx.Err())
		}
	}
	if r.languageRuntime != nil {
		closeErr = errors.Join(closeErr, r.languageRuntime.Close())
	}
	if r.outbox != nil {
		closeErr = errors.Join(closeErr, r.outbox.Close())
	}
	if r.closePool != nil {
		r.closePool()
	}
	return closeErr
}

func loadProcessConfig(getenv func(string) string) (processConfig, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	addr := strings.TrimSpace(getenv("REALTIME_ADDR"))
	if addr == "" {
		addr = defaultAddr
	}
	ticketSecret := strings.TrimSpace(getenv("REALTIME_TICKET_SECRET"))
	if len([]byte(ticketSecret)) < minTicketSecretBytes {
		return processConfig{}, fmt.Errorf("REALTIME_TICKET_SECRET must contain at least %d bytes", minTicketSecretBytes)
	}
	// none (default): subtitles only, force mock TTS.
	// pcm: real TTS PCM over DataChannel (Chrome-safe, no Opus encoder).
	// opus: WebRTC Opus send track with pure-Go PCM encoding.
	downlink := strings.ToLower(strings.TrimSpace(getenv("REALTIME_TTS_DOWNLINK")))
	skipTTS := true
	forceMock := true
	codec := "none"
	mode := "none"
	switch downlink {
	case "opus":
		mode, skipTTS, forceMock, codec = "opus", false, false, "opus"
	case "pcm", "datachannel", "dc":
		mode, skipTTS, forceMock, codec = "pcm", true, false, "pcm"
	}
	source := strings.TrimSpace(getenv("REALTIME_SOURCE_LANGUAGE"))
	if source == "" {
		source = "zh-CN"
	}
	target := strings.TrimSpace(getenv("REALTIME_TARGET_LANGUAGE"))
	if target == "" {
		target = "en-US"
	}
	return processConfig{
		Addr: addr, TicketSecret: ticketSecret, SkipTTSTrack: skipTTS, ForceMockTTS: forceMock,
		DownlinkMode: mode, DownlinkCodec: codec,
		SourceLanguage: source, TargetLanguage: target,
		MetricsToken: strings.TrimSpace(getenv("REALTIME_METRICS_TOKEN")),
	}, nil
}

func webrtcConfigSampleRate(cfg processConfig) int {
	switch cfg.DownlinkMode {
	case "pcm":
		return 24000
	default:
		return 48000
	}
}

// applySubtitleOnlyOverrides keeps Aliyun TTS from running while downlink is none.
func applySubtitleOnlyOverrides(cfg processConfig, providers config.ProviderConfig) config.ProviderConfig {
	if !cfg.ForceMockTTS {
		return providers
	}
	providers.TTS.Provider = config.ProviderMock
	return providers
}

func newControlPlaneHandler(ticketSecret string) (http.Handler, error) {
	cfg, err := loadProcessConfig(func(key string) string {
		switch key {
		case "REALTIME_TICKET_SECRET":
			return ticketSecret
		default:
			return os.Getenv(key)
		}
	})
	if err != nil {
		return nil, err
	}
	return newControlPlaneHandlerWithConfig(cfg)
}

func newControlPlaneHandlerWithConfig(cfg processConfig) (http.Handler, error) {
	application, err := newControlPlaneRuntimeWithConfig(context.Background(), cfg)
	if err != nil {
		return nil, err
	}
	return application.Handler, nil
}

func newControlPlaneRuntimeWithConfig(ctx context.Context, cfg processConfig) (application *controlPlaneRuntime, returnErr error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resources := &controlPlaneResources{}
	defer func() {
		if returnErr != nil {
			_ = resources.Close(context.Background())
		}
	}()
	now := func() time.Time { return time.Now().UTC() }

	codec, err := realtimev1.NewHMACTicketCodec(realtimev1.TicketConfig{
		Secret: []byte(cfg.TicketSecret),
		TTL:    time.Minute,
		Now:    now,
	})
	if err != nil {
		return nil, fmt.Errorf("configure ticket codec: %w", err)
	}
	tickets, err := webrtc.NewHMACTicketValidator(codec)
	if err != nil {
		return nil, fmt.Errorf("configure ticket validator: %w", err)
	}

	controlHandler := controlchannel.NewHandler()
	factory, err := webrtc.NewPionTransportFactory(webrtc.PionTransportConfig{
		ICEServers: []webrtc.ICEServerConfig{{
			URLs: []string{"stun:stun.l.google.com:19302"},
		}},
		Media: webrtc.MediaConfig{
			SkipTTSTrack:  cfg.SkipTTSTrack,
			DownlinkCodec: cfg.DownlinkCodec,
		},
		Control: webrtc.ControlConfig{Handler: controlHandler},
	})
	if err != nil {
		return nil, fmt.Errorf("configure pion transport factory: %w", err)
	}
	connections := webrtc.NewMemoryConnectionManager(factory)

	signaling, err := webrtc.NewSignalingService(webrtc.Dependencies{
		Tickets:     tickets,
		Connections: connections,
		Now:         now,
	})
	if err != nil {
		return nil, fmt.Errorf("configure signaling: %w", err)
	}

	runtimeBridge := &localruntime.LifecycleRuntimeBridge{}
	staticLanguages := localruntime.StaticLanguageConfigReader{
		Source: cfg.SourceLanguage, Target: cfg.TargetLanguage, Now: now,
	}
	var languages session.LanguageConfigReader = staticLanguages
	var sessions session.SessionReader = localruntime.TrustSessionReader{}
	var durableFinalTurns recordsv1.FinalTurnSink
	var fallbackReplays controlplane.FallbackPlaybackReplayStore
	var speechCatalogLoader *localruntime.PostgresSpeechCatalogLoader
	var speechRouteResolver speech.RouteResolver
	if apiDatabaseEnabled(os.Getenv) {
		databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
		if databaseURL == "" {
			return nil, fmt.Errorf("REALTIME_API_DATABASE is enabled but DATABASE_URL is empty")
		}
		pool, err := pgxpool.New(ctx, databaseURL)
		if err != nil {
			return nil, fmt.Errorf("open DATABASE_URL for realtime records: %w", err)
		}
		resources.closePool = pool.Close
		if err := pool.Ping(ctx); err != nil {
			pool.Close()
			resources.closePool = nil
			return nil, fmt.Errorf("ping DATABASE_URL for realtime records: %w", err)
		}
		slog.Info("realtime-audio linked to API database",
			"final_turn_outbox", true,
			"session_reader", "postgres",
			"language_reader", "postgres",
		)
		sessions = localruntime.PostgresSessionReader{Pool: pool}
		languages = localruntime.PostgresLanguageConfigReader{Pool: pool, Now: now}
		durableFinalTurns = pipeline.NewPostgresFinalTurnSink(pool)
		fallbackReplays = localruntime.PostgresFallbackPlaybackReplayStore{Pool: pool}
		speechCatalogLoader = localruntime.NewPostgresSpeechCatalogLoader(pool)
		speechRouteResolver = localruntime.NewPostgresSpeechRouteResolver(pool)
	}

	metricRegistry := realtimemetrics.Default()
	liveFinalTurns := localruntime.DataChannelFinalTurnSink{Media: connections, Failures: metricRegistry}
	var finalTurns recordsv1.FinalTurnSink = liveFinalTurns
	if durableFinalTurns != nil {
		finalTurns = localruntime.FanoutFinalTurnSink{Durable: durableFinalTurns, Live: liveFinalTurns}
	}
	sinks, err := runtime.OpenSinksFromEnv(ctx)
	if err != nil {
		return nil, fmt.Errorf("open realtime outbox: %w", err)
	}
	resources.outbox = sinks.Outbox
	var usage pipeline.UsageFactSink = &localruntime.MemoryUsageSink{}
	if usageOutboxEnabled(os.Getenv) {
		usage = sinks.Usage
		slog.Info("realtime-audio usage outbox enabled", "backend", os.Getenv("REALTIME_OUTBOX"))
	}

	providerConfig, err := config.LoadProviderConfigFromEnvironment()
	if err != nil {
		return nil, fmt.Errorf("load provider config: %w", err)
	}
	providerConfig = applySubtitleOnlyOverrides(cfg, providerConfig)
	// Local Silero (or energy) VAD owns utterance cuts; disable Qwen server_vad unless set.
	if strings.TrimSpace(os.Getenv("ASR_SERVER_VAD")) == "" {
		providerConfig.ASR.ServerVAD = false
	}
	var speechBindings *speech.BindingCoordinator
	if speechCatalogLoader != nil {
		catalog, err := speechCatalogLoader.LoadSpeechCatalog(ctx)
		if err != nil {
			return nil, fmt.Errorf("load speech catalog: %w", err)
		}
		var registry *speech.ProviderRegistry
		if cfg.ForceMockTTS {
			registry, _, err = localruntime.BuildSpeechRegistryWithMockTTS(catalog, providerConfig)
		} else {
			registry, _, err = localruntime.BuildSpeechRegistry(catalog, providerConfig)
		}
		if err != nil {
			return nil, fmt.Errorf("build speech registry: %w", err)
		}
		speechBindings, err = speech.NewBindingCoordinator(registry, speechRouteResolver)
		if err != nil {
			return nil, fmt.Errorf("create speech binding coordinator: %w", err)
		}
	}

	var audioSink pipeline.AudioChunkSink = localruntime.DiscardAudioSink{}
	switch cfg.DownlinkMode {
	case "opus":
		audioSink = localruntime.PlaybackAudioSink{Media: connections}
	case "pcm":
		sampleRate := providerConfig.TTS.SampleRate
		if sampleRate <= 0 {
			sampleRate = 24000
		}
		audioSink = &localruntime.DataChannelTTSAudioSink{Media: connections, SampleRate: sampleRate, Failures: metricRegistry}
	}

	voiceID := strings.TrimSpace(providerConfig.TTS.Voice)
	if voiceID == "" {
		voiceID = "Cherry"
	}

	newSegmenter, err := newLocalVADSegmenterFactory(os.Getenv)
	if err != nil {
		return nil, fmt.Errorf("configure local VAD: %w", err)
	}
	var playbackInterrupter runtime.PlaybackInterrupter
	if candidate, ok := audioSink.(runtime.PlaybackInterrupter); ok {
		playbackInterrupter = candidate
	}

	managerDependencies := runtime.Dependencies{
		FrameSources: localruntime.WebRTCFrameSources{
			Media:          connections,
			SourceLanguage: cfg.SourceLanguage,
			Languages:      languages,
		},
		NewSegmenter: newSegmenter,
		// Command recognition uses an isolated lightweight classifier. It is
		// intentionally separate from the rolling ordinary-VAD classifier.
		NewCommandClassifier: func() (vad.Classifier, error) {
			return localruntime.EnergySpeechClassifier{Threshold: 0.01}, nil
		},
		Languages:           languages,
		FinalTurns:          finalTurns,
		AssistantReplies:    localruntime.DataChannelAssistantReplySink{Media: connections, Failures: metricRegistry},
		ModeChanges:         realtimemetrics.ObserveModeChangedSink(sinks.ModeChanges, metricRegistry),
		Usage:               usage,
		Audio:               audioSink,
		PlaybackInterrupter: playbackInterrupter,
		Runtime:             runtimeBridge,
		VoiceID:             voiceID,
		Logger:              slog.Default(),
		Latency:             slog.Default(),
		ProviderFailures:    metricRegistry,
		Lifecycle:           metricRegistry,
		ModeCommands:        metricRegistry,
		Now:                 now,
	}
	if speechBindings != nil {
		managerDependencies.SpeechBindings = speechBindings
	}
	manager, err := runtime.NewManager(providerConfig, mockOfflineProviders(cfg.SourceLanguage), managerDependencies)
	if err != nil {
		return nil, fmt.Errorf("configure runtime manager: %w", err)
	}
	if err := controlHandler.SetModeControl(manager); err != nil {
		return nil, fmt.Errorf("configure WebRTC control channel: %w", err)
	}
	if speechBindings != nil {
		if err := resources.startLanguageConfigConsumer(ctx, speechBindings); err != nil {
			return nil, err
		}
	}

	lifecycle, err := session.NewLifecycleService(session.Dependencies{
		Sessions:    sessions,
		Runtimes:    session.NewMemoryRuntimeRepository(),
		Pipelines:   manager,
		Connections: connections,
		Now:         now,
	})
	if err != nil {
		return nil, fmt.Errorf("configure lifecycle: %w", err)
	}
	runtimeBridge.Set(lifecycle)

	handler, err := controlplane.New(controlplane.Dependencies{
		Lifecycle:       lifecycle,
		Modes:           manager,
		Fallback:        manager,
		FallbackReplays: fallbackReplays,
		Signaling:       signaling,
		Connections:     connections,
		Tickets:         tickets,
		Config: localruntime.StaticWebRTCConfig{
			ICEServers: []controlplane.ICEServer{{
				URLs: []string{"stun:stun.l.google.com:19302"},
			}},
			Now:           now,
			UplinkCodec:   "opus",
			DownlinkCodec: cfg.DownlinkCodec,
			SampleRateHz:  webrtcConfigSampleRate(cfg),
			Channels:      1,
		},
		Now: now,
	})
	if err != nil {
		return nil, fmt.Errorf("configure control-plane: %w", err)
	}
	mux := http.NewServeMux()
	realtimemetrics.Register(mux, metricRegistry, cfg.MetricsToken)
	mux.Handle("/", handler)
	return &controlPlaneRuntime{Handler: mux, resources: resources}, nil
}

func (r *controlPlaneResources) startLanguageConfigConsumer(ctx context.Context, bindings *speech.BindingCoordinator) error {
	streamRuntime, err := languageevents.OpenRuntimeFromEnv(ctx)
	if err != nil {
		return fmt.Errorf("open language config event consumer: %w", err)
	}
	r.languageRuntime = streamRuntime
	consumer, err := languageevents.NewConsumer(streamRuntime.Stream, bindings, slog.Default())
	if err != nil {
		return fmt.Errorf("create language config event consumer: %w", err)
	}
	consumerCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	r.languageCancel = cancel
	r.languageDone = done
	go func() {
		defer close(done)
		if err := consumer.Run(consumerCtx); err != nil {
			slog.Error("language config event consumer stopped", "error", err)
		}
	}()
	slog.Info("language config event consumer started")
	return nil
}

func apiDatabaseEnabled(getenv func(string) string) bool {
	if getenv == nil {
		getenv = os.Getenv
	}
	switch strings.ToLower(strings.TrimSpace(getenv("REALTIME_API_DATABASE"))) {
	case "1", "true", "yes", "enabled", "on":
		return true
	default:
		return false
	}
}

func usageOutboxEnabled(getenv func(string) string) bool {
	if getenv == nil {
		getenv = os.Getenv
	}
	switch strings.ToLower(strings.TrimSpace(getenv("REALTIME_OUTBOX"))) {
	case "valkey":
		return true
	default:
		return false
	}
}

const (
	localVADSilenceAfter  = 800 * time.Millisecond
	localVADMaxDuration   = 12 * time.Second
	localVADPrefixPadding = 500 * time.Millisecond
)

// newLocalVADSegmenterFactory wires the process-local utterance cutter used by
// every realtime session. Silero is the default production classifier; energy
// remains an explicit LOCAL_VAD_PROVIDER=energy fallback for environments
// without ONNX Runtime.
func newLocalVADSegmenterFactory(getenv func(string) string) (runtime.SegmenterFactory, error) {
	cfg := silero.LoadLocalConfigFromEnv(getenv)
	options := vad.Options{
		SilenceAfter:  localVADSilenceAfter,
		MaxDuration:   localVADMaxDuration,
		PrefixPadding: localVADPrefixPadding,
	}
	switch cfg.Provider {
	case silero.ProviderEnergy:
		slog.Info("realtime-audio local VAD provider", "provider", silero.ProviderEnergy)
		return func() (*vad.Segmenter, error) {
			return vad.NewSegmenter(localruntime.EnergySpeechClassifier{Threshold: 0.01}, options)
		}, nil
	case silero.ProviderSilero:
		if err := silero.EnsureAssets(&cfg); err != nil {
			return nil, fmt.Errorf("prepare silero VAD assets: %w", err)
		}
		rt, err := silero.NewRuntime(silero.RuntimeConfig{
			LibraryPath:  cfg.LibraryPath,
			ModelPath:    cfg.ModelPath,
			Threshold:    cfg.Threshold,
			NegThreshold: cfg.NegThreshold,
		})
		if err != nil {
			return nil, err
		}
		slog.Info("realtime-audio local VAD provider",
			"provider", silero.ProviderSilero,
			"model_path", cfg.ModelPath,
			"library_path", cfg.LibraryPath,
			"threshold", cfg.Threshold,
		)
		return func() (*vad.Segmenter, error) {
			classifier, err := rt.NewClassifier()
			if err != nil {
				return nil, err
			}
			return vad.NewSegmenter(classifier, options)
		}, nil
	default:
		return nil, fmt.Errorf("unsupported LOCAL_VAD_PROVIDER %q (want %q or %q)", cfg.Provider, silero.ProviderSilero, silero.ProviderEnergy)
	}
}

func mockOfflineProviders(sourceLanguage string) config.Providers {
	sourceLanguage = strings.TrimSpace(sourceLanguage)
	if sourceLanguage == "" {
		sourceLanguage = "zh-CN"
	}
	sourceText := "你好"
	translated := "Hello"
	assistantReply := "你好，我是小灵。"
	if !strings.HasPrefix(strings.ToLower(sourceLanguage), "zh") {
		sourceText = "Hello"
		translated = "你好"
		assistantReply = "Hello, I am Lingow."
	}
	return config.Providers{
		ASR: asr.NewFakeProvider(asr.FakeProviderConfig{
			Final: asr.FinalResult{
				Text:           sourceText,
				SourceLanguage: sourceLanguage,
				Provider:       "mock-asr",
				Model:          "fake",
			},
		}),
		Assistant: assistant.NewFakeProvider(assistant.FakeProviderConfig{Result: assistant.Result{
			Text: assistantReply, Language: sourceLanguage, Provider: "mock-llm", Model: "fake",
		}}),
		Translation: &translate.FakeProvider{
			Result: translate.Result{Text: translated, Provider: "mock-llm", Model: "fake"},
		},
		TTS: tts.NewFakeProvider(tts.FakeProviderConfig{
			Chunks: []tts.AudioChunk{{SequenceNo: 1, Data: []byte{0, 0, 0, 0}}},
			Result: tts.Result{Provider: "mock-tts", Model: "fake"},
		}),
	}
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: httpReadHeaderTimeout,
		ReadTimeout:       httpReadTimeout,
		WriteTimeout:      httpWriteTimeout,
		IdleTimeout:       httpIdleTimeout,
	}
}
