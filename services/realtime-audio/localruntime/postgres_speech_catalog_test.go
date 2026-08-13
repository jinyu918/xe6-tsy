package localruntime

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestPostgresSpeechCatalogLoaderLoadsActiveCatalog(t *testing.T) {
	now := time.Date(2026, time.August, 13, 12, 0, 0, 0, time.UTC)
	queryer := &speechCatalogQueryFake{responses: []speechCatalogQueryResponse{
		{rows: &speechCatalogRowsFake{rows: [][]any{{
			"asr-1", "aliyun", "qwen-asr", []string{"en-US", "zh-CN"}, false, true,
			"pcm_s16le", 16000, 1, true, nil, now, now,
		}}}},
		{rows: &speechCatalogRowsFake{rows: [][]any{{
			"tts-1", "aliyun", "qwen-tts", "Cherry", []string{"en-US", "zh-CN"}, true,
			"pcm_s16le", 24000, 1, true, nil, now, now,
		}}}},
		{rows: &speechCatalogRowsFake{rows: [][]any{{
			"route-1", "en-US", "zh-CN", "asr-1", "tts-1", true, nil,
		}}}},
	}}
	loader := NewPostgresSpeechCatalogLoader(queryer)

	catalog, err := loader.LoadSpeechCatalog(t.Context())
	if err != nil {
		t.Fatalf("LoadSpeechCatalog() error = %v", err)
	}
	if len(catalog.ASRProfiles) != 1 || len(catalog.TTSProfiles) != 1 || len(catalog.Routes) != 1 {
		t.Fatalf("catalog = %#v", catalog)
	}
	if catalog.ASRProfiles[0].ID != "asr-1" || catalog.TTSProfiles[0].VoiceID != "Cherry" || catalog.Routes[0].ASRProfileID != "asr-1" {
		t.Fatalf("catalog = %#v", catalog)
	}
	if len(queryer.queries) != 3 {
		t.Fatalf("query count = %d, want 3", len(queryer.queries))
	}
	for _, query := range queryer.queries {
		if !strings.Contains(query, "enabled = TRUE") || !strings.Contains(query, "retired_at IS NULL") {
			t.Fatalf("catalog query does not filter active rows: %s", query)
		}
	}
}

func TestPostgresSpeechCatalogLoaderRejectsUnavailableDependencies(t *testing.T) {
	if _, err := NewPostgresSpeechCatalogLoader(nil).LoadSpeechCatalog(t.Context()); !errors.Is(err, ErrSpeechCatalogReaderRequired) {
		t.Fatalf("nil loader dependency error = %v", err)
	}
	if _, err := (*PostgresSpeechCatalogLoader)(nil).LoadSpeechCatalog(t.Context()); !errors.Is(err, ErrSpeechCatalogReaderRequired) {
		t.Fatalf("nil loader error = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := NewPostgresSpeechCatalogLoader(&speechCatalogQueryFake{}).LoadSpeechCatalog(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("LoadSpeechCatalog(canceled) error = %v", err)
	}
}

func TestPostgresSpeechCatalogLoaderReturnsQueryAndScanErrors(t *testing.T) {
	queryErr := errors.New("database unavailable")
	loader := NewPostgresSpeechCatalogLoader(&speechCatalogQueryFake{responses: []speechCatalogQueryResponse{{err: queryErr}}})
	if _, err := loader.LoadSpeechCatalog(t.Context()); !errors.Is(err, queryErr) {
		t.Fatalf("LoadSpeechCatalog(query error) = %v", err)
	}

	scanErr := errors.New("unsupported scan value")
	loader = NewPostgresSpeechCatalogLoader(&speechCatalogQueryFake{responses: []speechCatalogQueryResponse{{
		rows: &speechCatalogRowsFake{rows: [][]any{{"asr-1"}}, scanErr: scanErr},
	}}})
	if _, err := loader.LoadSpeechCatalog(t.Context()); !errors.Is(err, scanErr) {
		t.Fatalf("LoadSpeechCatalog(scan error) = %v", err)
	}

	rowsErr := errors.New("connection lost while reading")
	loader = NewPostgresSpeechCatalogLoader(&speechCatalogQueryFake{responses: []speechCatalogQueryResponse{{
		rows: &speechCatalogRowsFake{err: rowsErr},
	}}})
	if _, err := loader.LoadSpeechCatalog(t.Context()); !errors.Is(err, rowsErr) {
		t.Fatalf("LoadSpeechCatalog(rows error) = %v", err)
	}
}

type speechCatalogQueryResponse struct {
	rows pgx.Rows
	err  error
}

type speechCatalogQueryFake struct {
	responses []speechCatalogQueryResponse
	queries   []string
}

func (q *speechCatalogQueryFake) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	q.queries = append(q.queries, sql)
	if len(q.responses) == 0 {
		return nil, errors.New("unexpected query")
	}
	response := q.responses[0]
	q.responses = q.responses[1:]
	return response.rows, response.err
}

type speechCatalogRowsFake struct {
	rows    [][]any
	index   int
	err     error
	scanErr error
	closed  bool
}

func (r *speechCatalogRowsFake) Close()                                       { r.closed = true }
func (r *speechCatalogRowsFake) Err() error                                   { return r.err }
func (r *speechCatalogRowsFake) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *speechCatalogRowsFake) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *speechCatalogRowsFake) Next() bool {
	if r.index >= len(r.rows) {
		r.closed = true
		return false
	}
	r.index++
	return true
}

func (r *speechCatalogRowsFake) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	if r.index == 0 || r.index > len(r.rows) {
		return errors.New("scan called before Next")
	}
	values := r.rows[r.index-1]
	if len(values) != len(dest) {
		return errors.New("scan column count mismatch")
	}
	for index, value := range values {
		if value == nil {
			continue
		}
		target := reflect.ValueOf(dest[index])
		if target.Kind() != reflect.Pointer || target.IsNil() {
			return errors.New("scan destination must be a non-nil pointer")
		}
		target = target.Elem()
		source := reflect.ValueOf(value)
		if source.Type().AssignableTo(target.Type()) {
			target.Set(source)
			continue
		}
		if source.Type().ConvertibleTo(target.Type()) {
			target.Set(source.Convert(target.Type()))
			continue
		}
		return errors.New("scan value type mismatch")
	}
	return nil
}

func (r *speechCatalogRowsFake) Values() ([]any, error) { return nil, nil }
func (r *speechCatalogRowsFake) RawValues() [][]byte    { return nil }
func (r *speechCatalogRowsFake) Conn() *pgx.Conn        { return nil }

var _ speechCatalogQueryer = (*speechCatalogQueryFake)(nil)
var _ pgx.Rows = (*speechCatalogRowsFake)(nil)
