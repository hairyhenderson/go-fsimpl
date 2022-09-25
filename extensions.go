package fsimpl

import (
	"context"
	"io/fs"
	"mime"
	"net/http"
	"path/filepath"
	"sync"

	"github.com/hairyhenderson/go-fsimpl/internal"
)

// WithContextFS injects a context into the filesystem fs, if the filesystem
// supports it (i.e. has a WithContext method). This can be used to propagate
// cancellation.
func WithContextFS(ctx context.Context, fsys fs.FS) fs.FS {
	if cfsys, ok := fsys.(internal.WithContexter); ok {
		return cfsys.WithContext(ctx)
	}

	return fsys
}

// WithHeaderFS injects the given HTTP header into the filesystem fs, if the
// filesystem supports it (i.e. has a WithHeader method).
func WithHeaderFS(headers http.Header, fsys fs.FS) fs.FS {
	if cfsys, ok := fsys.(internal.WithHeaderer); ok {
		return cfsys.WithHeader(headers)
	}

	return fsys
}

// WithHTTPClientFS injects an HTTP client into the filesystem fs, if the
// filesystem supports it (i.e. has a WithHTTPClient method).
func WithHTTPClientFS(client *http.Client, fsys fs.FS) fs.FS {
	if cfsys, ok := fsys.(internal.WithHTTPClienter); ok {
		return cfsys.WithHTTPClient(client)
	}

	return fsys
}

// common types we want to be able to handle which can be missing by default
//
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

// ContentType returns the MIME content type for the given [io/fs.FileInfo]. If
// fi has a ContentType method, it will be used first, otherwise the filename's
// extension will be used. See the docs for [mime.TypeByExtension] for details
// on how extension lookup works.
//
// The returned value may have parameters (e.g.
// "application/json; charset=utf-8") which can be parsed with
// [mime.ParseMediaType].
func ContentType(fi fs.FileInfo) string {
	ct := ""
	if cf, ok := fi.(internal.ContentTypeFileInfo); ok {
		ct = cf.ContentType()
	}

	if ct != "" {
		return ct
	}

	extraMimeInit.Do(func() {
		for k, v := range extraMimeTypes {
			_ = mime.AddExtensionType(k, v)
		}
	})

	// fall back to guessing based on extension
	ext := filepath.Ext(fi.Name())
	ct = mime.TypeByExtension(ext)

	return ct
}
