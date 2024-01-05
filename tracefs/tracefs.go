// Package tracefs instruments a filesystem for distributed tracing operations.
// The OpenTelemetry API is supported.
//
// This is not strictly a filesystem implementation, but rather a wrapper
// around an existing filesystem. As such, it does not implement the
// [fsimpl.FSProvider] interface.
//
// # Usage
//
// To use this filesystem, call [New] with a base filesystem. All operations on
// the returned filesystem will be instrumented.
//
// In order to report traces, an OTel [trace.TracerProvider] must first be set
// up. The details of this are outside the scope of this module, but see the
// fscli example in this repository's examples directory for one approach.
//
// A [trace.TracerProvider] can optionally be passed to [New] using
// [WithTracerProvider].
//
// # Propagation
//
// By default, this filesystem will use the global [propagation.TextMapPropagator]
// to extract trace information from the context. This can be overridden by
// passing a [propagation.TextMapPropagator] to [WithPropagators].
package tracefs

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/hairyhenderson/go-fsimpl"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

type traceFS struct {
	ctx         context.Context
	fsys        fs.FS
	tracer      trace.Tracer
	propagators propagation.TextMapPropagator
}

const tracerName = "github.com/hairyhenderson/go-fsimpl/tracefs"

// New returns a filesystem (an fs.FS) that instruments the given filesystem,
// adding trace spans for each operation. The given context will be used for
// the root span. Options can be provided to configure the behaviour of the
// instrumented filesystem.
func New(ctx context.Context, fsys fs.FS, opts ...Option) (fs.FS, error) {
	cfg := config{}
	for _, opt := range opts {
		opt.apply(&cfg)
	}

	if cfg.tp == nil {
		cfg.tp = otel.GetTracerProvider()
	}

	if cfg.propagators == nil {
		cfg.propagators = otel.GetTextMapPropagator()
	}

	tfsys := traceFS{
		ctx:         ctx,
		fsys:        fsys,
		tracer:      cfg.tp.Tracer(tracerName),
		propagators: cfg.propagators,
	}

	return &tfsys, nil
}

type urlFS interface {
	URL() string
}

var (
	_ fs.FS         = (*traceFS)(nil)
	_ fs.ReadDirFS  = (*traceFS)(nil)
	_ fs.ReadFileFS = (*traceFS)(nil)
	_ fs.GlobFS     = (*traceFS)(nil)
	_ fs.SubFS      = (*traceFS)(nil)
	_ fs.StatFS     = (*traceFS)(nil)
)

func fsattribs(fsys fs.FS, name string) trace.SpanStartEventOption {
	if ufs, ok := fsys.(urlFS); ok {
		return trace.WithAttributes(
			Path(name),
			BaseURL(ufs.URL()),
			Type(fmt.Sprintf("%T", fsys)),
		)
	}

	return trace.WithAttributes(Path(name), Type(fmt.Sprintf("%T", fsys)))
}

func (f *traceFS) Open(name string) (fs.File, error) {
	ctx, span := f.tracer.Start(f.ctx, "fs.Open", fsattribs(f.fsys, name))
	defer span.End()

	fsys := fsimpl.WithContextFS(ctx, f.fsys)

	file, err := fsys.Open(name)
	if err != nil {
		return file, recordError(span, err)
	}

	return wrapFile(ctx, file, fsys, name, f.tracer)
}

func (f *traceFS) ReadDir(name string) ([]fs.DirEntry, error) {
	ctx, span := f.tracer.Start(f.ctx, "fs.ReadDir", fsattribs(f.fsys, name))
	defer span.End()

	fsys := fsimpl.WithContextFS(ctx, f.fsys)

	des, err := fs.ReadDir(fsys, name)

	span.SetAttributes(DirEntries(len(des)))

	return des, recordError(span, err)
}

func (f *traceFS) ReadFile(name string) ([]byte, error) {
	ctx, span := f.tracer.Start(f.ctx, "fs.ReadFile", fsattribs(f.fsys, name))
	defer span.End()

	fsys := fsimpl.WithContextFS(ctx, f.fsys)

	b, err := fs.ReadFile(fsys, name)

	span.SetAttributes(
		FileSize(int64(len(b))),
		FileBytesRead(len(b)),
	)

	return b, recordError(span, err)
}

func (f *traceFS) Stat(name string) (fs.FileInfo, error) {
	ctx, span := f.tracer.Start(f.ctx, "fs.Stat", fsattribs(f.fsys, name))
	defer span.End()

	fsys := fsimpl.WithContextFS(ctx, f.fsys)

	fi, err := fs.Stat(fsys, name)

	span.SetAttributes(
		FileSize(fi.Size()),
		FilePerms(fi.Mode().String()),
		FileModTime(fi.ModTime()),
	)

	return fi, recordError(span, err)
}

func (f *traceFS) Sub(dir string) (fs.FS, error) {
	ctx, span := f.tracer.Start(f.ctx, "fs.Sub", fsattribs(f.fsys, dir))
	defer span.End()

	fsys := fsimpl.WithContextFS(ctx, f.fsys)

	sub, err := fs.Sub(fsys, dir)
	if err == nil {
		// wrap the sub-fs in a traceFS
		sub = &traceFS{ctx: ctx, fsys: sub, tracer: f.tracer, propagators: f.propagators}
	}

	return sub, recordError(span, err)
}

func globattribs(fsys fs.FS, name string) trace.SpanStartEventOption {
	if ufs, ok := fsys.(urlFS); ok {
		return trace.WithAttributes(Pattern(name), BaseURL(ufs.URL()))
	}

	return trace.WithAttributes(Pattern(name))
}

func (f *traceFS) Glob(pattern string) ([]string, error) {
	ctx, span := f.tracer.Start(f.ctx, "fs.Glob", globattribs(f.fsys, pattern))
	defer span.End()

	fsys := fsimpl.WithContextFS(ctx, f.fsys)

	matches, err := fs.Glob(fsys, pattern)

	return matches, recordError(span, err)
}

// recordError records the given error on the span, and returns it. It does not
// set the span's status to error.
func recordError(span trace.Span, err error) error {
	span.RecordError(err)

	return err
}
