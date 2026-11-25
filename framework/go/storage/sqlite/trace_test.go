package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/heiyeluren/mz-gui/framework/go/observability/tracecontext"
)

func TestOpenStartsSQLiteOpenAndMigrateSpans(t *testing.T) {
	var spans []tracecontext.Span
	ctx := tracecontext.WithObserver(context.Background(), func(span tracecontext.Span) {
		spans = append(spans, span)
	})
	database, err := Open(ctx, Options{
		Path: filepath.Join(t.TempDir(), "trace.db"),
		Migrations: fstest.MapFS{
			"0001_widgets.sql": &fstest.MapFile{Data: []byte("CREATE TABLE widgets (id TEXT PRIMARY KEY);")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if len(spans) < 2 {
		t.Fatalf("Open/migrate 必须启动 sqlite span: %#v", spans)
	}
	if spans[0].Kind != tracecontext.OperationSQLite || spans[0].Component != "sqlite" || spans[0].Operation != "sqlite.open" {
		t.Fatalf("open span 错误: %#v", spans[0])
	}
	if spans[1].Kind != tracecontext.OperationSQLite || spans[1].Operation != "sqlite.migrate" {
		t.Fatalf("migrate span 错误: %#v", spans[1])
	}
	if spans[0].Trace.TraceID == "" || spans[1].Trace.TraceID != spans[0].Trace.TraceID {
		t.Fatalf("migrate 必须继承 open TraceID: open=%#v migrate=%#v", spans[0].Trace, spans[1].Trace)
	}
	if _, ok := spans[0].SafeAttributes["path"]; ok {
		t.Fatal("sqlite span 不得记录 path")
	}
}

func TestOpenContinuesIncomingParentTrace(t *testing.T) {
	parent, err := tracecontext.New(true)
	if err != nil {
		t.Fatal(err)
	}
	parentCtx, err := tracecontext.WithContext(context.Background(), parent)
	if err != nil {
		t.Fatal(err)
	}
	var got tracecontext.Span
	ctx := tracecontext.WithObserver(parentCtx, func(span tracecontext.Span) {
		if span.Operation == "sqlite.open" {
			got = span
		}
	})
	database, err := Open(ctx, Options{
		Path:       filepath.Join(t.TempDir(), "parent-trace.db"),
		Migrations: fstest.MapFS{"0001_init.sql": &fstest.MapFile{Data: []byte("CREATE TABLE items (id TEXT);")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	database.Close()
	if got.Trace.TraceID != parent.TraceID || got.Trace.ParentSpanID != parent.SpanID {
		t.Fatalf("Open 必须继承父 trace: parent=%#v span=%#v", parent, got.Trace)
	}
}
