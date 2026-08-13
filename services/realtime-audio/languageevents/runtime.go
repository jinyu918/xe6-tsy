package languageevents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/redis/go-redis/v9"
)

const (
	redisModeStandalone = "standalone"
	redisModeCluster    = "cluster"
)

// Runtime owns the Valkey client used only by the language-configuration
// consumer. It is intentionally separate from the realtime outbox because
// consuming API facts and publishing realtime facts have different lifecycles.
type Runtime struct {
	Stream *ValkeyStream
	close  func() error
}

// Close releases the client after its Consumer has stopped receiving entries.
func (r *Runtime) Close() error {
	if r == nil || r.close == nil {
		return nil
	}
	return r.close()
}

// OpenRuntimeFromEnv connects the language-config consumer to the configured
// standalone Redis/Valkey node or Cluster. The stream group is created by
// NewValkeyStream so a new realtime deployment can begin consuming safely.
func OpenRuntimeFromEnv(ctx context.Context) (*Runtime, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	redisURL := strings.TrimSpace(os.Getenv("REDIS_URL"))
	if redisURL == "" {
		return nil, errors.New("open language config event runtime: REDIS_URL is required")
	}
	client, err := openRedisClient(redisURL, os.Getenv("REALTIME_REDIS_MODE"))
	if err != nil {
		return nil, fmt.Errorf("open language config event runtime: %w", err)
	}
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("open language config event runtime: ping Valkey: %w", err)
	}
	stream, err := NewValkeyStream(
		ctx,
		client,
		os.Getenv("LINGOW_LANGUAGE_CONFIG_CHANGED_STREAM"),
		os.Getenv("LINGOW_LANGUAGE_CONFIG_CHANGED_GROUP"),
		os.Getenv("LINGOW_LANGUAGE_CONFIG_CHANGED_CONSUMER"),
	)
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("open language config event runtime: create consumer group: %w", err)
	}
	return &Runtime{Stream: stream, close: client.Close}, nil
}

func openRedisClient(rawURL, mode string) (redis.UniversalClient, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", redisModeStandalone:
		options, err := redis.ParseURL(strings.TrimSpace(rawURL))
		if err != nil {
			return nil, err
		}
		return redis.NewClient(options), nil
	case redisModeCluster:
		options, err := redis.ParseClusterURL(strings.TrimSpace(rawURL))
		if err != nil {
			return nil, err
		}
		return redis.NewClusterClient(options), nil
	default:
		return nil, fmt.Errorf("unsupported REALTIME_REDIS_MODE %q (supported: standalone, cluster)", mode)
	}
}
