package tracefs

import (
	"context"
	"io"
	"io/fs"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// wrapFile wraps a fs.File with a tracedelegate
func wrapFile(ctx context.Context, f fs.File, fsys fs.FS, name string, tracer trace.Tracer) (fs.File, error) {
	delegate := tracedelegate{ctx: ctx, fsys: fsys, tracer: tracer, name: name}

	switch ft := f.(type) {
	case fs.ReadDirFile:
		return &traceDir{f: ft, d: delegate}, nil
	case io.ReaderAt:
		if _, ok := ft.(io.Seeker); ok {
			return &traceReaderAtSeekerFile{f: f, d: delegate}, nil
		}

		return &traceReaderAtFile{f: f, d: delegate}, nil
	case io.Seeker:
		return &traceSeekerFile{f: f, d: delegate}, nil
	default:
		return &traceFile{f: ft, d: delegate}, nil
	}
}

type traceFile struct {
	f fs.File
	d tracedelegate
}

type traceDir struct {
	f fs.ReadDirFile
	d tracedelegate
}

type traceReaderAtFile struct {
	f fs.File
	d tracedelegate
}

type traceSeekerFile struct {
	f fs.File
	d tracedelegate
}

type traceReaderAtSeekerFile struct {
	f fs.File
	d tracedelegate
}

var (
	_ fs.ReadDirFile = (*traceDir)(nil)
	_ fs.File        = (*traceReaderAtFile)(nil)
	_ io.ReaderAt    = (*traceReaderAtFile)(nil)
	_ fs.File        = (*traceSeekerFile)(nil)
	_ io.Seeker      = (*traceSeekerFile)(nil)
	_ fs.File        = (*traceReaderAtSeekerFile)(nil)
	_ io.ReaderAt    = (*traceReaderAtSeekerFile)(nil)
	_ io.Seeker      = (*traceReaderAtSeekerFile)(nil)
)

type tracedelegate struct {
	ctx    context.Context
	fsys   fs.FS
	tracer trace.Tracer
	name   string
}

// common file functions
func (d tracedelegate) close(f fs.File) error {
	_, span := d.tracer.Start(d.ctx, "file.Close", fsattribs(d.fsys, d.name))
	defer span.End()

	return recordError(span, f.Close())
}

func (d tracedelegate) read(f fs.File, p []byte) (int, error) {
	_, span := d.tracer.Start(d.ctx, "file.Read", fsattribs(d.fsys, d.name))
	defer span.End()

	n, err := f.Read(p)

	span.SetAttributes(FileBytesRead(n))

	return n, recordError(span, err)
}

func (d tracedelegate) stat(f fs.File) (fs.FileInfo, error) {
	_, span := d.tracer.Start(d.ctx, "file.Stat", fsattribs(d.fsys, d.name))
	defer span.End()

	fi, err := f.Stat()

	span.SetAttributes(
		FileSize(fi.Size()),
		FilePerms(fi.Mode().String()),
		FileModTime(fi.ModTime()),
	)

	return fi, recordError(span, err)
}

func (d tracedelegate) readDir(f fs.ReadDirFile, n int) ([]fs.DirEntry, error) {
	_, span := d.tracer.Start(d.ctx, "file.ReadDir", fsattribs(d.fsys, d.name))
	defer span.End()

	ents, err := f.ReadDir(n)

	span.SetAttributes(DirEntries(len(ents)))

	return ents, recordError(span, err)
}

func (d tracedelegate) readAt(f fs.File, p []byte, off int64) (int, error) {
	ra := f.(io.ReaderAt)

	_, span := d.tracer.Start(d.ctx, "file.ReadAt", fsattribs(d.fsys, d.name))
	defer span.End()

	n, err := ra.ReadAt(p, off)

	span.SetAttributes(FileBytesRead(n))

	return n, recordError(span, err)
}

func (d tracedelegate) seek(f fs.File, off int64, whence int) (int64, error) {
	s := f.(io.Seeker)

	_, span := d.tracer.Start(d.ctx, "file.Seek", fsattribs(d.fsys, d.name))
	defer span.End()

	n, err := s.Seek(off, whence)

	span.SetAttributes(
		attribute.Int64("file.offset", off),
		attribute.Int("file.seek_whence", whence),
		attribute.Int64("file.seek_result", n),
	)

	return n, recordError(span, err)
}

func (f *traceFile) Close() error               { return f.d.close(f.f) }
func (f *traceDir) Close() error                { return f.d.close(f.f) }
func (f *traceReaderAtFile) Close() error       { return f.d.close(f.f) }
func (f *traceSeekerFile) Close() error         { return f.d.close(f.f) }
func (f *traceReaderAtSeekerFile) Close() error { return f.d.close(f.f) }

func (f *traceFile) Read(p []byte) (int, error)               { return f.d.read(f.f, p) }
func (f *traceDir) Read(p []byte) (int, error)                { return f.d.read(f.f, p) }
func (f *traceReaderAtFile) Read(p []byte) (int, error)       { return f.d.read(f.f, p) }
func (f *traceSeekerFile) Read(p []byte) (int, error)         { return f.d.read(f.f, p) }
func (f *traceReaderAtSeekerFile) Read(p []byte) (int, error) { return f.d.read(f.f, p) }

func (f *traceFile) Stat() (fs.FileInfo, error)               { return f.d.stat(f.f) }
func (f *traceDir) Stat() (fs.FileInfo, error)                { return f.d.stat(f.f) }
func (f *traceReaderAtFile) Stat() (fs.FileInfo, error)       { return f.d.stat(f.f) }
func (f *traceSeekerFile) Stat() (fs.FileInfo, error)         { return f.d.stat(f.f) }
func (f *traceReaderAtSeekerFile) Stat() (fs.FileInfo, error) { return f.d.stat(f.f) }

func (f *traceDir) ReadDir(n int) ([]fs.DirEntry, error) { return f.d.readDir(f.f, n) }

func (f *traceReaderAtFile) ReadAt(p []byte, off int64) (int, error) { return f.d.readAt(f.f, p, off) }
func (f *traceReaderAtSeekerFile) ReadAt(p []byte, off int64) (int, error) {
	return f.d.readAt(f.f, p, off)
}

func (f *traceSeekerFile) Seek(offset int64, whence int) (int64, error) {
	return f.d.seek(f.f, offset, whence)
}

func (f *traceReaderAtSeekerFile) Seek(offset int64, whence int) (int64, error) {
	return f.d.seek(f.f, offset, whence)
}
