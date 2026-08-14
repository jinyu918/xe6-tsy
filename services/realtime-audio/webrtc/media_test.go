package webrtc

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/1024XEngineer/xe6-tsy/services/realtime-audio/audio"
	"github.com/1024XEngineer/xe6-tsy/services/realtime-audio/playback"
	"github.com/pion/rtp"
	pion "github.com/pion/webrtc/v4"
)

func TestPionEventSinkWaitsForOpenAndSendsJSON(t *testing.T) {
	channel := &dataChannelRecorder{state: pion.DataChannelStateConnecting}
	sink := newPionEventSink(channel)
	event := playback.Event{EventID: "event-1", Type: playback.EventStarted, SessionID: "session-1", PlaybackID: "playback-1", SequenceNo: 1, OccurredAt: time.Unix(1700000000, 0).UTC()}
	sent := make(chan error, 1)
	go func() { sent <- sink.Publish(context.Background(), event) }()
	select {
	case err := <-sent:
		t.Fatalf("Publish() returned before open: %v", err)
	case <-time.After(10 * time.Millisecond):
	}
	channel.Open()
	if err := <-sent; err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if len(channel.Messages()) != 1 || !contains(channel.Messages()[0], `"event_id":"event-1"`) {
		t.Fatalf("messages = %#v", channel.Messages())
	}
}

func TestPionEventSinkSerializesDataChannelWrites(t *testing.T) {
	channel := &blockingDataChannelRecorder{started: make(chan struct{}, 2), release: make(chan struct{})}
	sink := newPionEventSink(channel)
	event := playback.Event{EventID: "event-1", Type: playback.EventStarted, SessionID: "session-1", PlaybackID: "playback-1", SequenceNo: 1}
	firstDone := make(chan error, 1)
	go func() { firstDone <- sink.Publish(context.Background(), event) }()
	select {
	case <-channel.started:
	case <-time.After(time.Second):
		t.Fatal("first event did not reach DataChannel")
	}
	secondDone := make(chan error, 1)
	go func() { secondDone <- sink.Publish(context.Background(), event) }()
	select {
	case <-channel.started:
		t.Fatal("second event wrote while first DataChannel write was blocked")
	case <-time.After(100 * time.Millisecond):
	}
	close(channel.release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first Publish() error = %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second Publish() error = %v", err)
	}
}

func TestPionEventSinkPublishUnblocksWhenTransportCloses(t *testing.T) {
	peer := &mediaPeerRecorder{
		fakePionPeerConnection: &fakePionPeerConnection{gatherComplete: closedChannel()},
		dataChannelState:       pion.DataChannelStateConnecting,
	}
	factory := newFakePionTransportFactory(peer.fakePionPeerConnection)
	factory.newPeerConnection = func(pion.Configuration) (pionPeerConnection, error) { return peer, nil }
	transport, err := factory.Create(context.Background(), "session-1", "rtc_1", nil)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	sink := transport.(*PionTransport).TranslationEvents()
	event := playback.Event{EventID: "event-1", Type: playback.EventStarted, SessionID: "session-1", PlaybackID: "playback-1", SequenceNo: 1}
	publishDone := make(chan error, 1)
	go func() { publishDone <- sink.Publish(context.Background(), event) }()
	select {
	case err := <-publishDone:
		t.Fatalf("Publish() returned before transport close: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	if err := transport.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case err := <-publishDone:
		if !errors.Is(err, ErrTransportClosed) {
			t.Fatalf("Publish() after transport close error = %v, want ErrTransportClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Publish() remained blocked after transport close")
	}
}

func TestPionAudioSourceDrainsBufferedFramesAfterClose(t *testing.T) {
	decoder := &fakeRTPDecoder{pcm: []byte{1, 2}}
	source, err := newPionAudioSource(decoder, func() time.Time { return time.Unix(1700000000, 0).UTC() })
	if err != nil {
		t.Fatalf("newPionAudioSource() error = %v", err)
	}
	frames := make([]audio.Frame, 3)
	for index := range frames {
		frames[index], err = audio.NewFrame([]byte{byte(index + 1), 0}, defaultASRSampleRate, time.Unix(int64(index+1), 0).UTC())
		if err != nil {
			t.Fatalf("audio.NewFrame() error = %v", err)
		}
		source.frames <- frames[index]
	}
	source.closeDone()
	for index := range frames {
		frame, readErr := source.ReadFrame(context.Background())
		if readErr != nil {
			t.Fatalf("ReadFrame(%d) error = %v", index, readErr)
		}
		if !reflect.DeepEqual(frame, frames[index]) {
			t.Fatalf("ReadFrame(%d) = %#v, want %#v", index, frame, frames[index])
		}
	}
	if _, err := source.ReadFrame(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("ReadFrame(after drain) error = %v, want io.EOF", err)
	}
}

func TestPionAudioSourceDecodesRemoteRTP(t *testing.T) {
	track := &remoteTrackRecorder{packets: make(chan *rtp.Packet, 1)}
	decoder := &fakeRTPDecoder{pcm: []byte{1, 2, 3, 4}}
	source, err := newPionAudioSource(decoder, func() time.Time { return time.Unix(1700000000, 0).UTC() })
	if err != nil {
		t.Fatalf("newPionAudioSource() error = %v", err)
	}
	if err := source.Attach(track); err != nil {
		t.Fatalf("Attach() error = %v", err)
	}
	track.packets <- &rtp.Packet{Header: rtp.Header{Timestamp: 10}, Payload: []byte{9}}
	frame, err := source.ReadFrame(context.Background())
	if err != nil {
		t.Fatalf("ReadFrame() error = %v", err)
	}
	if !reflect.DeepEqual(frame.PCM, decoder.pcm) || frame.SampleRate != 16_000 {
		t.Fatalf("frame = %#v", frame)
	}
	if err := source.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	close(track.packets)
	if _, err := source.ReadFrame(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("ReadFrame(after close) error = %v", err)
	}
}

func TestOpusDecoderDecodesWebRTCSilencePacket(t *testing.T) {
	decoder, err := NewOpusDecoder()
	if err != nil {
		t.Fatalf("NewOpusDecoder() error = %v", err)
	}
	pcm, err := decoder.Decode([]byte{0xf8, 0xff, 0xfe})
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if len(pcm) == 0 || len(pcm)%2 != 0 {
		t.Fatalf("PCM length = %d", len(pcm))
	}
}

func TestPionFactoryConfiguresMediaWhenPeerSupportsIt(t *testing.T) {
	peer := &mediaPeerRecorder{fakePionPeerConnection: &fakePionPeerConnection{
		answer: pion.SessionDescription{Type: pion.SDPTypeAnswer, SDP: "answer-sdp"}, gatherComplete: closedChannel(),
	}}
	factory := newFakePionTransportFactory(peer.fakePionPeerConnection)
	factory.newPeerConnection = func(pion.Configuration) (pionPeerConnection, error) { return peer, nil }
	transport, err := factory.Create(context.Background(), "session-1", "rtc_1", nil)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	mediaTransport, ok := transport.(*PionTransport)
	if !ok || mediaTransport.AudioSource() == nil || mediaTransport.TranslationEvents() == nil {
		t.Fatalf("media transport = %#v", transport)
	}
	if _, err := transport.Answer(context.Background(), SessionDescription{Type: "offer", SDP: opusOfferSDP()}); err != nil {
		t.Fatalf("Answer() error = %v", err)
	}
	if mediaTransport.TTSAudioTrack() == nil || mediaTransport.Playback() == nil {
		t.Fatalf("TTS media was not configured after remote offer")
	}
	if len(peer.trackAdds) != 1 || peer.dataChannelLabel != defaultDataChannelLabel {
		t.Fatalf("media setup: tracks=%d label=%q", len(peer.trackAdds), peer.dataChannelLabel)
	}
}

func TestPionTransportAnswerConfiguresTTSTrackOnceConcurrently(t *testing.T) {
	peer := &blockingMediaPeerRecorder{
		mediaPeerRecorder: &mediaPeerRecorder{fakePionPeerConnection: &fakePionPeerConnection{
			answer: pion.SessionDescription{Type: pion.SDPTypeAnswer, SDP: "answer-sdp"}, gatherComplete: closedChannel(),
		}},
		addStarted: make(chan struct{}, 2), release: make(chan struct{}),
	}
	factory := newFakePionTransportFactory(peer.fakePionPeerConnection)
	factory.newPeerConnection = func(pion.Configuration) (pionPeerConnection, error) { return peer, nil }
	transport, err := factory.Create(context.Background(), "session-1", "rtc_1", nil)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	answers := make(chan error, 2)
	answer := func() {
		_, answerErr := transport.Answer(context.Background(), SessionDescription{Type: "offer", SDP: opusOfferSDP()})
		answers <- answerErr
	}
	go answer()
	select {
	case <-peer.addStarted:
	case <-time.After(time.Second):
		t.Fatal("first Answer() did not register a TTS track")
	}
	go answer()
	select {
	case <-peer.addStarted:
		t.Fatal("second Answer() registered a duplicate TTS track")
	case <-time.After(20 * time.Millisecond):
	}
	close(peer.release)
	for range 2 {
		if err := <-answers; err != nil {
			t.Fatalf("Answer() error = %v", err)
		}
	}
	if got := peer.trackCount(); got != 1 {
		t.Fatalf("TTS track registrations = %d, want 1", got)
	}
}

func TestPionTransportCloseUnblocksAudioSource(t *testing.T) {
	peer := &mediaPeerRecorder{fakePionPeerConnection: &fakePionPeerConnection{gatherComplete: closedChannel()}}
	factory := newFakePionTransportFactory(peer.fakePionPeerConnection)
	factory.newPeerConnection = func(pion.Configuration) (pionPeerConnection, error) { return peer, nil }
	transport, err := factory.Create(context.Background(), "session-1", "rtc_1", nil)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	source := transport.(*PionTransport).AudioSource()
	if err := transport.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := source.ReadFrame(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("ReadFrame() error = %v, want io.EOF", err)
	}
}

func contains(value, part string) bool {
	return strings.Contains(value, part)
}

type dataChannelRecorder struct {
	mu       sync.Mutex
	state    pion.DataChannelState
	onOpen   func()
	messages []string
}

type blockingDataChannelRecorder struct {
	started chan struct{}
	release chan struct{}
}

func (d *blockingDataChannelRecorder) OnOpen(func()) {}
func (*blockingDataChannelRecorder) ReadyState() pion.DataChannelState {
	return pion.DataChannelStateOpen
}
func (d *blockingDataChannelRecorder) SendText(string) error {
	d.started <- struct{}{}
	<-d.release
	return nil
}

func (d *dataChannelRecorder) OnOpen(handler func()) { d.mu.Lock(); d.onOpen = handler; d.mu.Unlock() }
func (d *dataChannelRecorder) ReadyState() pion.DataChannelState {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.state
}
func (d *dataChannelRecorder) SendText(message string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.messages = append(d.messages, message)
	return nil
}
func (d *dataChannelRecorder) Open() {
	d.mu.Lock()
	d.state = pion.DataChannelStateOpen
	handler := d.onOpen
	d.mu.Unlock()
	if handler != nil {
		handler()
	}
}
func (d *dataChannelRecorder) Messages() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.messages...)
}

type fakeRTPDecoder struct{ pcm []byte }

func (d *fakeRTPDecoder) Decode([]byte) ([]byte, error) { return append([]byte(nil), d.pcm...), nil }

type remoteTrackRecorder struct{ packets chan *rtp.Packet }

func (r *remoteTrackRecorder) ReadRTP() (*rtp.Packet, error) {
	packet, ok := <-r.packets
	if !ok {
		return nil, io.EOF
	}
	return packet, nil
}

type mediaPeerRecorder struct {
	*fakePionPeerConnection
	trackAdds        []pion.TrackLocal
	dataChannelLabel string
	dataChannel      *dataChannelRecorder
	dataChannelState pion.DataChannelState
	onTrack          func(pionRemoteTrack)
}

func (p *mediaPeerRecorder) AddTrack(track pion.TrackLocal) (*pion.RTPSender, error) {
	p.trackAdds = append(p.trackAdds, track)
	return nil, nil
}
func (p *mediaPeerRecorder) CreateDataChannel(label string, _ *pion.DataChannelInit) (pionDataChannel, error) {
	p.dataChannelLabel = label
	state := p.dataChannelState
	if state == pion.DataChannelStateUnknown {
		state = pion.DataChannelStateOpen
	}
	p.dataChannel = &dataChannelRecorder{state: state}
	return p.dataChannel, nil
}
func (p *mediaPeerRecorder) OnTrack(handler func(pionRemoteTrack)) { p.onTrack = handler }

var _ pionMediaPeerConnection = (*mediaPeerRecorder)(nil)

type blockingMediaPeerRecorder struct {
	*mediaPeerRecorder
	addStarted chan struct{}
	release    chan struct{}
	mu         sync.Mutex
	adds       int
}

func (p *blockingMediaPeerRecorder) AddTrack(track pion.TrackLocal) (*pion.RTPSender, error) {
	p.mu.Lock()
	p.adds++
	p.mu.Unlock()
	p.addStarted <- struct{}{}
	<-p.release
	return nil, nil
}

func (p *blockingMediaPeerRecorder) trackCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.adds
}

var _ pionMediaPeerConnection = (*blockingMediaPeerRecorder)(nil)
