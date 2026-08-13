package localruntime

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/1024XEngineer/xe6-tsy/services/realtime-audio/speech"
	"github.com/jackc/pgx/v5"
)

func TestPostgresSpeechRouteResolverReadsCurrentRouteOnEachResolve(t *testing.T) {
	queryer := &speechRouteQueryFake{rows: []speechRouteRow{
		{values: []any{"en-US", "zh-CN", "asr-1", "tts-1"}},
		{values: []any{"en-US", "zh-CN", "asr-2", "tts-2"}},
	}}
	resolver := NewPostgresSpeechRouteResolver(queryer)

	first, err := resolver.ResolveBinding(t.Context(), "zh-CN", "en-US")
	if err != nil {
		t.Fatalf("first ResolveBinding() error = %v", err)
	}
	if first.ASRProfileID != "asr-1" || first.TTSProfileID != "tts-1" {
		t.Fatalf("first route = %#v", first)
	}

	second, err := resolver.ResolveBinding(t.Context(), "en-US", "zh-CN")
	if err != nil {
		t.Fatalf("second ResolveBinding() error = %v", err)
	}
	if second.ASRProfileID != "asr-2" || second.TTSProfileID != "tts-2" {
		t.Fatalf("second route = %#v", second)
	}
	if queryer.calls != 2 {
		t.Fatalf("QueryRow calls = %d, want 2", queryer.calls)
	}
	for _, call := range queryer.queryCalls {
		if !strings.Contains(call.sql, "enabled = TRUE") || !strings.Contains(call.sql, "retired_at IS NULL") {
			t.Fatalf("route query does not filter active rows: %s", call.sql)
		}
		if !reflect.DeepEqual(call.args, []any{"en-US", "zh-CN"}) {
			t.Fatalf("route query args = %#v, want canonical pair", call.args)
		}
	}
}

func TestPostgresSpeechRouteResolverMapsNotFoundAndDependencyErrors(t *testing.T) {
	if _, err := NewPostgresSpeechRouteResolver(nil).ResolveBinding(t.Context(), "zh-CN", "en-US"); !errors.Is(err, ErrSpeechRouteReaderRequired) {
		t.Fatalf("nil resolver dependency error = %v", err)
	}
	if _, err := (*PostgresSpeechRouteResolver)(nil).ResolveBinding(t.Context(), "zh-CN", "en-US"); !errors.Is(err, ErrSpeechRouteReaderRequired) {
		t.Fatalf("nil resolver error = %v", err)
	}

	queryer := &speechRouteQueryFake{rows: []speechRouteRow{{err: pgx.ErrNoRows}}}
	if _, err := NewPostgresSpeechRouteResolver(queryer).ResolveBinding(t.Context(), "zh-CN", "en-US"); !errors.Is(err, speech.ErrSpeechRouteNotFound) {
		t.Fatalf("ResolveBinding(not found) error = %v", err)
	}

	queryErr := errors.New("database unavailable")
	queryer = &speechRouteQueryFake{rows: []speechRouteRow{{err: queryErr}}}
	if _, err := NewPostgresSpeechRouteResolver(queryer).ResolveBinding(t.Context(), "zh-CN", "en-US"); !errors.Is(err, queryErr) {
		t.Fatalf("ResolveBinding(query error) = %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := NewPostgresSpeechRouteResolver(&speechRouteQueryFake{}).ResolveBinding(ctx, "zh-CN", "en-US"); !errors.Is(err, context.Canceled) {
		t.Fatalf("ResolveBinding(canceled) error = %v", err)
	}
}

type speechRouteQueryCall struct {
	sql  string
	args []any
}

type speechRouteRow struct {
	values []any
	err    error
}

type speechRouteQueryFake struct {
	rows       []speechRouteRow
	calls      int
	queryCalls []speechRouteQueryCall
}

func (q *speechRouteQueryFake) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	q.calls++
	q.queryCalls = append(q.queryCalls, speechRouteQueryCall{sql: sql, args: append([]any(nil), args...)})
	if len(q.rows) == 0 {
		return speechRouteRowFake{err: errors.New("unexpected query")}
	}
	row := q.rows[0]
	q.rows = q.rows[1:]
	return speechRouteRowFake{values: row.values, err: row.err}
}

type speechRouteRowFake struct {
	values []any
	err    error
}

func (r speechRouteRowFake) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(r.values) != len(dest) {
		return errors.New("scan column count mismatch")
	}
	for index, value := range r.values {
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

var _ speechRouteQueryer = (*speechRouteQueryFake)(nil)
