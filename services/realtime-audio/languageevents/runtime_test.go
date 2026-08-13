package languageevents

import (
	"context"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestOpenRuntimeFromEnvUsesConfiguredLanguageConfigConsumer(t *testing.T) {
	server, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run() error = %v", err)
	}
	t.Cleanup(server.Close)

	t.Setenv("REDIS_URL", "redis://"+server.Addr())
	t.Setenv("REALTIME_REDIS_MODE", redisModeStandalone)
	t.Setenv("LINGOW_LANGUAGE_CONFIG_CHANGED_STREAM", "lingow:language:config:changed:test")
	t.Setenv("LINGOW_LANGUAGE_CONFIG_CHANGED_GROUP", "language-config-test")
	t.Setenv("LINGOW_LANGUAGE_CONFIG_CHANGED_CONSUMER", "realtime-test-1")

	runtime, err := OpenRuntimeFromEnv(t.Context())
	if err != nil {
		t.Fatalf("OpenRuntimeFromEnv() error = %v", err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	if runtime.Stream == nil {
		t.Fatal("Stream = nil")
	}
	if runtime.Stream.stream != "lingow:language:config:changed:test" ||
		runtime.Stream.group != "language-config-test" ||
		runtime.Stream.consumer != "realtime-test-1" {
		t.Fatalf("stream config = %#v", runtime.Stream)
	}
}

func TestOpenRuntimeFromEnvValidatesConnectionConfiguration(t *testing.T) {
	t.Setenv("REDIS_URL", "")
	if _, err := OpenRuntimeFromEnv(t.Context()); err == nil || !strings.Contains(err.Error(), "REDIS_URL is required") {
		t.Fatalf("missing REDIS_URL error = %v", err)
	}

	t.Setenv("REDIS_URL", "redis://localhost:6379")
	t.Setenv("REALTIME_REDIS_MODE", "sentinel")
	if _, err := OpenRuntimeFromEnv(context.Background()); err == nil || !strings.Contains(err.Error(), "unsupported REALTIME_REDIS_MODE") {
		t.Fatalf("unsupported mode error = %v", err)
	}
}

func TestOpenRedisClientSelectsConfiguredTopology(t *testing.T) {
	standalone, err := openRedisClient("redis://localhost:6379/0", redisModeStandalone)
	if err != nil {
		t.Fatalf("open standalone client: %v", err)
	}
	t.Cleanup(func() { _ = standalone.Close() })
	if _, ok := standalone.(*redis.Client); !ok {
		t.Fatalf("standalone client = %T", standalone)
	}

	cluster, err := openRedisClient("redis://user:secret@localhost:6379?addr=localhost:6380", redisModeCluster)
	if err != nil {
		t.Fatalf("open cluster client: %v", err)
	}
	t.Cleanup(func() { _ = cluster.Close() })
	clusterClient, ok := cluster.(*redis.ClusterClient)
	if !ok || len(clusterClient.Options().Addrs) != 2 {
		t.Fatalf("cluster client = %T, options = %#v", cluster, clusterClient)
	}
}
