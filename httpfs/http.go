package httpfs

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"time"

	"github.com/hairyhenderson/go-fsimpl"
	"github.com/hairyhenderson/go-fsimpl/internal"
)

type httpFS struct {
	ctx     context.Context
	base    *url.URL
	client  *http.Client
	headers http.Header
}

// New provides a filesystem (an fs.FS) for the HTTP (or HTTPS) endpoint
// rooted at u. This filesystem is suitable for use with the 'http' or
// 'https' URL schemes. All reads are made with the GET method, while stat calls
// are made with the HEAD method (with a fallback to GET).
//
// A context can be given by using WithContextFS.
// HTTP Headers can be provided by using WithHeaderFS.
func New(u *url.URL) (fs.FS, error) {
	return &httpFS{
		ctx:     context.Background(),
		client:  http.DefaultClient,
		base:    u,
		headers: http.Header{},
	}, nil
}

// FS is used to register this filesystem with an fsimpl.FSMux
//
//nolint:gochecknoglobals
var FS = fsimpl.FSProviderFunc(New, "http", "https")

var (
	_ fs.FS                     = (*httpFS)(nil)
	_ fs.ReadFileFS             = (*httpFS)(nil)
	_ fs.SubFS                  = (*httpFS)(nil)
	_ internal.WithContexter    = (*httpFS)(nil)
	_ internal.WithHeaderer     = (*httpFS)(nil)
	_ internal.WithHTTPClienter = (*httpFS)(nil)
)

func (f httpFS) URL() string {
	return f.base.String()
}

func (f httpFS) WithContext(ctx context.Context) fs.FS {
	fsys := f
	fsys.ctx = ctx

	return &fsys
}

func (f httpFS) WithHeader(headers http.Header) fs.FS {
	fsys := f
	if len(fsys.headers) == 0 {
		fsys.headers = headers
	} else {
		for k, vs := range fsys.headers {
			for _, v := range vs {
				fsys.headers.Add(k, v)
			}
		}
	}

	return &fsys
}

func (f httpFS) WithHTTPClient(client *http.Client) fs.FS {
	fsys := f
	fsys.client = client

	return &fsys
}

func (f httpFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{
			Op:   "open",
			Path: name,
			Err:  fs.ErrInvalid,
		}
	}

	u, err := f.subURL(name)
	if err != nil {
		return nil, err
	}

	return &httpFile{
		ctx:    f.ctx,
		u:      u,
		client: f.client,
		name:   name,
		hdr:    f.headers,
	}, nil
}

func (f httpFS) ReadFile(name string) ([]byte, error) {
	opened, err := f.Open(name)
	if err != nil {
		return nil, err
	}
	defer opened.Close()

	b, err := io.ReadAll(opened)
	if err != nil {
		return nil, err
	}

	return b, nil
}

func (f httpFS) Sub(name string) (fs.FS, error) {
	fsys := f

	u, err := f.subURL(name)
	if err != nil {
		return nil, err
	}

	fsys.base = u

	return &fsys, nil
}

func (f *httpFS) subURL(name string) (*url.URL, error) {
	rel, err := url.Parse(name)
	if err != nil {
		return nil, err
	}

	return f.base.ResolveReference(rel), nil
}

type httpFile struct {
	ctx    context.Context
	body   io.ReadCloser
	fi     fs.FileInfo
	u      *url.URL
	client *http.Client
	hdr    http.Header
	name   string
}

var _ fs.File = (*httpFile)(nil)

func (f *httpFile) request(method string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(f.ctx, method, f.u.String(), nil)
	if err != nil {
		return nil, err
	}

	req.Header = f.hdr

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}

	modTime := time.Time{}
	if mod := resp.Header.Get("Last-Modified"); mod != "" {
		// best-effort - if it can't be parsed, just ignore it...
		modTime, _ = http.ParseTime(mod)
	}

	f.fi = internal.FileInfo(f.name, resp.ContentLength, 0o644, modTime, resp.Header.Get("Content-Type"))

	if resp.StatusCode == 0 || resp.StatusCode >= 400 {
		return nil, fmt.Errorf("http GET failed with status %d", resp.StatusCode)
	}

	// The response body must be closed later
	return resp.Body, nil
}

func (f *httpFile) Close() error {
	if f.body == nil {
		return nil
	}

	return f.body.Close()
}

func (f *httpFile) Read(p []byte) (int, error) {
	if f.body == nil {
		body, err := f.request(http.MethodGet)
		if err != nil {
			return 0, err
		}

		f.body = body
	}

	return f.body.Read(p)
}

func (f *httpFile) Stat() (fs.FileInfo, error) {
	body, err := f.request(http.MethodHead)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	return f.fi, nil
}
