package fsimpl

import (
	"context"
	"io/fs"
	"mime"
	"net/http"
	"path/filepath"
	"sync"
)

type withContexter interface {
	WithContext(ctx context.Context) fs.FS
}

// WithContextFS injects a context into the filesystem fs, if the filesystem
// supports it (i.e. has a WithContext method).
func WithContextFS(ctx context.Context, fsys fs.FS) fs.FS {
	if cfsys, ok := fsys.(withContexter); ok {
		return cfsys.WithContext(ctx)
	}

	return fsys
}

type withHeaderer interface {
	WithHeader(headers http.Header) fs.FS
}

// WithHeaderFS injects a context into the filesystem fs, if the filesystem
// supports it (i.e. has a WithHeader method).
func WithHeaderFS(headers http.Header, fsys fs.FS) fs.FS {
	if cfsys, ok := fsys.(withHeaderer); ok {
		return cfsys.WithHeader(headers)
	}

	return fsys
}

// common types we want to be able to handle which can be missing by default
//nolint:gochecknoglobals
var (
	extraMimeTypes = map[string]string{
		".yml":  "application/yaml",
		".yaml": "application/yaml",
		".csv":  "text/csv",
		".toml": "application/toml",
		".env":  "application/x-env",
		".txt":  "text/plain",
	}
	extraMimeInit sync.Once
)

type contentTypeFileInfo interface {
	fs.FileInfo

	ContentType() string
}

// ContentType returns the MIME content type for the given fs.FileInfo. If fi
// has a ContentType method, that will be used, otherwise the type will be
// guessed by the filename's extension. See the docs for mime.TypeByExtension
// for details on how extension lookup works.
// Some additional
//
// The returned value may have parameters (e.g. "application/json; charset=utf-8")
// which can be parsed with mime.ParseMediaType.
func ContentType(fi fs.FileInfo) string {
	if cf, ok := fi.(contentTypeFileInfo); ok {
		return cf.ContentType()
	}

	extraMimeInit.Do(func() {
		for k, v := range extraMimeTypes {
			_ = mime.AddExtensionType(k, v)
		}
	})

	// fall back to guessing based on extension
	ext := filepath.Ext(fi.Name())

	return mime.TypeByExtension(ext)
}
