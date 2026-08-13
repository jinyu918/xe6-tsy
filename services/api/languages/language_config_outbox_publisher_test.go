package languages

import (
	"context"
	"errors"
	"testing"

	"github.com/redis/go-redis/v9"
)

type languageConfigStreamClientStub struct {
	args *redis.XAddArgs
	err  error
}

func (s *languageConfigStreamClientStub) XAdd(ctx context.Context, args *redis.XAddArgs) *redis.StringCmd {
	s.args = args
	command := redis.NewStringCmd(ctx)
	if s.err != nil {
		command.SetErr(s.err)
	} else {
		command.SetVal("1-0")
	}
	return command
}

func TestValkeyLanguageConfigChangedPublisherWritesCanonicalPayload(t *testing.T) {
	client := &languageConfigStreamClientStub{}
	publisher := NewValkeyLanguageConfigChangedPublisher(client, "language-config-test")
	payload := []byte(`{"event_id":"event-1"}`)

	if err := publisher.PublishLanguageConfigChanged(t.Context(), payload); err != nil {
		t.Fatalf("PublishLanguageConfigChanged() error = %v", err)
	}
	if client.args == nil || client.args.Stream != "language-config-test" {
		t.Fatalf("XADD args = %#v, want configured stream", client.args)
	}
	if got, ok := client.args.Values.(map[string]any)["payload"].([]byte); !ok || string(got) != string(payload) {
		t.Fatalf("XADD payload = %#v, want canonical payload", client.args.Values)
	}
}

func TestValkeyLanguageConfigChangedPublisherUsesDefaultStream(t *testing.T) {
	client := &languageConfigStreamClientStub{}
	publisher := NewValkeyLanguageConfigChangedPublisher(client, "")

	if err := publisher.PublishLanguageConfigChanged(t.Context(), []byte(`{}`)); err != nil {
		t.Fatalf("PublishLanguageConfigChanged() error = %v", err)
	}
	if client.args.Stream != defaultLanguageConfigChangedStream {
		t.Fatalf("stream = %q, want %q", client.args.Stream, defaultLanguageConfigChangedStream)
	}
}

func TestValkeyLanguageConfigChangedPublisherReturnsBrokerError(t *testing.T) {
	brokerErr := errors.New("Valkey unavailable")
	publisher := NewValkeyLanguageConfigChangedPublisher(&languageConfigStreamClientStub{err: brokerErr}, "stream")

	if err := publisher.PublishLanguageConfigChanged(t.Context(), []byte(`{}`)); !errors.Is(err, brokerErr) {
		t.Fatalf("PublishLanguageConfigChanged() error = %v, want %v", err, brokerErr)
	}
}
