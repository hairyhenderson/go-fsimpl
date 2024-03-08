package tracefs

import (
	"context"
	"io"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

//nolint:gochecknoglobals
var (
	prop     = propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})
	exporter = tracetest.NewInMemoryExporter()
	tp       = sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
)

func attribmap(kvs []attribute.KeyValue) map[string]interface{} {
	m := make(map[string]interface{}, len(kvs))

	for _, attr := range kvs {
		m[string(attr.Key)] = attr.Value.AsInterface()
	}

	return m
}

type fsysWithURL struct {
	fstest.MapFS
	url string
}

func (f *fsysWithURL) URL() string {
	return f.url
}

func TestTraceFS_Open(t *testing.T) {
	ctx := context.Background()

	exporter.Reset()

	fsys := fstest.MapFS{
		"foo/bar": {Data: []byte("hello")},
		"baz":     {Data: []byte("world"), Mode: 0o777},
	}

	tfsys, err := New(ctx, fsys, WithPropagators(prop), WithTracerProvider(tp))
	require.NoError(t, err)

	f, err := tfsys.Open("foo/bar")
	require.NoError(t, err)

	defer f.Close()

	b, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), b)

	spans := exporter.GetSpans()

	assert.Equal(t, "fs.Open", spans[0].Name)
	assert.Equal(t, "file.Read", spans[1].Name)
	assert.Equal(t, map[string]interface{}{
		"fs.path": "foo/bar",
		"fs.type": "fstest.MapFS",
	}, attribmap(spans[0].Attributes))
}

func TestTraceFS_Open_URLFS(t *testing.T) {
	ctx := context.Background()

	exporter.Reset()

	fsys := &fsysWithURL{fstest.MapFS{
		"foo/bar": {Data: []byte("hello")},
		"baz":     {Data: []byte("world"), Mode: 0o777},
	}, "mem:///"}

	tfsys, err := New(ctx, fsys, WithPropagators(prop), WithTracerProvider(tp))
	require.NoError(t, err)

	f, err := tfsys.Open("baz")
	require.NoError(t, err)

	defer f.Close()

	_, err = f.Stat()
	require.NoError(t, err)

	spans := exporter.GetSpans()

	assert.Equal(t, "fs.Open", spans[0].Name)
	assert.Equal(t, "file.Stat", spans[1].Name)
	assert.Equal(t, map[string]interface{}{
		"fs.base_url": "mem:///",
		"fs.path":     "baz",
		"fs.type":     "*tracefs.fsysWithURL",
	}, attribmap(spans[0].Attributes))
	assert.Equal(t, map[string]interface{}{
		"fs.base_url":  "mem:///",
		"fs.path":      "baz",
		"fs.type":      "*tracefs.fsysWithURL",
		"file.modtime": "0001-01-01T00:00:00Z",
		"file.perms":   "-rwxrwxrwx",
		"file.size":    int64(5),
	}, attribmap(spans[1].Attributes))

	// now a directory
	exporter.Reset()

	f, err = tfsys.Open("foo")
	require.NoError(t, err)

	defer f.Close()

	_, err = f.(fs.ReadDirFile).ReadDir(-1)
	require.NoError(t, err)

	spans = exporter.GetSpans()

	assert.Equal(t, "fs.Open", spans[0].Name)
	assert.Equal(t, "file.ReadDir", spans[1].Name)
	assert.Equal(t, map[string]interface{}{
		"fs.base_url": "mem:///",
		"fs.path":     "foo",
		"fs.type":     "*tracefs.fsysWithURL",
	}, attribmap(spans[0].Attributes))
}

func TestTraceFS_ReadDir(t *testing.T) {
	ctx := context.Background()

	exporter.Reset()

	fsys := fstest.MapFS{
		"foo/bar": {Data: []byte("hello")},
		"baz":     {Data: []byte("world"), Mode: 0o777},
	}

	tfsys, err := New(ctx, fsys, WithPropagators(prop), WithTracerProvider(tp))
	require.NoError(t, err)

	des, err := fs.ReadDir(tfsys, ".")
	require.NoError(t, err)

	assert.Len(t, des, 2)

	spans := exporter.GetSpans()

	assert.Equal(t, "fs.ReadDir", spans[0].Name)
	assert.Equal(t, map[string]interface{}{
		"fs.path":     ".",
		"fs.type":     "fstest.MapFS",
		"dir.entries": int64(2),
	}, attribmap(spans[0].Attributes))
}

func TestTraceFS_ReadFile(t *testing.T) {
	ctx := context.Background()

	exporter.Reset()

	fsys := fstest.MapFS{
		"foo/bar": {Data: []byte("hello")},
		"baz":     {Data: []byte("world"), Mode: 0o777},
	}

	tfsys, err := New(ctx, fsys, WithPropagators(prop), WithTracerProvider(tp))
	require.NoError(t, err)

	b, err := fs.ReadFile(tfsys, "foo/bar")
	require.NoError(t, err)

	assert.Equal(t, []byte("hello"), b)

	spans := exporter.GetSpans()

	assert.Equal(t, "fs.ReadFile", spans[0].Name)
	assert.Equal(t, map[string]interface{}{
		"fs.path":         "foo/bar",
		"fs.type":         "fstest.MapFS",
		"file.size":       int64(5),
		"file.bytes_read": int64(5),
	}, attribmap(spans[0].Attributes))
}

func TestTraceFS_Stat(t *testing.T) {
	ctx := context.Background()

	exporter.Reset()

	fsys := fstest.MapFS{"baz": {Data: []byte("world"), Mode: 0o777}}

	tfsys, err := New(ctx, fsys, WithPropagators(prop), WithTracerProvider(tp))
	require.NoError(t, err)

	fi, err := fs.Stat(tfsys, "baz")
	require.NoError(t, err)

	assert.Equal(t, int64(5), fi.Size())
	assert.Equal(t, fs.FileMode(0o777), fi.Mode())

	spans := exporter.GetSpans()

	assert.Equal(t, "fs.Stat", spans[0].Name)
	assert.Equal(t, map[string]interface{}{
		"fs.path":      "baz",
		"fs.type":      "fstest.MapFS",
		"file.size":    int64(5),
		"file.modtime": "0001-01-01T00:00:00Z",
		"file.perms":   "-rwxrwxrwx",
	}, attribmap(spans[0].Attributes))
}

func TestTraceFS_Sub(t *testing.T) {
	ctx := context.Background()

	exporter.Reset()

	fsys := fstest.MapFS{
		"foo/bar": {Data: []byte("hello")},
		"baz":     {Data: []byte("world"), Mode: 0o777},
	}

	tfsys, err := New(ctx, fsys, WithPropagators(prop), WithTracerProvider(tp))
	require.NoError(t, err)

	sub, err := fs.Sub(tfsys, "foo")
	require.NoError(t, err)

	b, err := fs.ReadFile(sub, "bar")
	require.NoError(t, err)

	assert.Equal(t, []byte("hello"), b)

	spans := exporter.GetSpans()

	assert.Equal(t, "fs.Sub", spans[0].Name)
	assert.Equal(t, "fs.ReadFile", spans[1].Name)
	assert.Equal(t, map[string]interface{}{
		"fs.path": "foo",
		"fs.type": "fstest.MapFS",
	}, attribmap(spans[0].Attributes))
	assert.Equal(t, map[string]interface{}{
		"fs.path":         "bar",
		"fs.type":         "*fs.subFS",
		"file.size":       int64(5),
		"file.bytes_read": int64(5),
	}, attribmap(spans[1].Attributes))
}

func TestTraceFS_Glob(t *testing.T) {
	ctx := context.Background()

	exporter.Reset()

	fsys := fstest.MapFS{
		"foo/bar": {Data: []byte("hello")},
		"baz":     {Data: []byte("world"), Mode: 0o777},
	}

	tfsys, err := New(ctx, fsys, WithPropagators(prop), WithTracerProvider(tp))
	require.NoError(t, err)

	matches, err := fs.Glob(tfsys, "*.txt")
	require.NoError(t, err)

	assert.Len(t, matches, 0)

	spans := exporter.GetSpans()

	assert.Equal(t, "fs.Glob", spans[0].Name)
	assert.Equal(t, map[string]interface{}{"fs.pattern": "*.txt"}, attribmap(spans[0].Attributes))

	exporter.Reset()

	matches, err = fs.Glob(tfsys, "*")
	require.NoError(t, err)

	assert.Len(t, matches, 2)

	spans = exporter.GetSpans()

	assert.Equal(t, "fs.Glob", spans[0].Name)
	assert.Equal(t, map[string]interface{}{"fs.pattern": "*"}, attribmap(spans[0].Attributes))
}

func TestTraceFS_Dir_Read(t *testing.T) {
	ctx := context.Background()

	exporter.Reset()

	fsys := fstest.MapFS{
		"foo/bar": {Data: []byte("hello")},
		"baz":     {Data: []byte("world"), Mode: 0o777},
	}

	tfsys, err := New(ctx, fsys, WithPropagators(prop), WithTracerProvider(tp))
	require.NoError(t, err)

	f, err := tfsys.Open(".")
	require.NoError(t, err)

	// Read doesn't work on a directory, so we expect an error.
	_, err = f.Read(make([]byte, 1))
	require.Error(t, err)

	spans := exporter.GetSpans()

	assert.Equal(t, "fs.Open", spans[0].Name)
	assert.Equal(t, map[string]interface{}{
		"fs.path": ".",
		"fs.type": "fstest.MapFS",
	}, attribmap(spans[0].Attributes))
	assert.Equal(t, "file.Read", spans[1].Name)
	assert.Equal(t, map[string]interface{}{
		"fs.path":         ".",
		"fs.type":         "fstest.MapFS",
		"file.bytes_read": int64(0),
	}, attribmap(spans[1].Attributes))
	assert.Equal(t, map[string]interface{}{
		"exception.message": "read .: invalid argument",
		"exception.type":    "*fs.PathError",
	}, attribmap(spans[1].Events[0].Attributes))

	exporter.Reset()

	fi, err := f.Stat()
	require.NoError(t, err)

	assert.True(t, fi.IsDir())

	spans = exporter.GetSpans()

	assert.Equal(t, "file.Stat", spans[0].Name)
	assert.Equal(t, map[string]interface{}{
		"fs.path":      ".",
		"fs.type":      "fstest.MapFS",
		"file.modtime": "0001-01-01T00:00:00Z",
		"file.perms":   "dr-xr-xr-x",
		"file.size":    int64(0),
	}, attribmap(spans[0].Attributes))
}
