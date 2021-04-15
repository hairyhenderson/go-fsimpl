package fsimpl

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func setupHTTP(t *testing.T) *httptest.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/hello.txt", func(w http.ResponseWriter, r *http.Request) {
		lmod, _ := time.Parse(time.RFC3339, "2021-04-01T12:00:00Z")
		w.Header().Set("Last-Modified", lmod.Format(http.TimeFormat))
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("hello world"))
	})

	mux.HandleFunc("/sub/subfile.json", func(w http.ResponseWriter, r *http.Request) {
		accept := r.Header.Get("Accept")
		if accept != "" {
			w.Header().Set("Content-Type", accept)
		}

		_, _ = w.Write([]byte(`{"msg": "hi there"}`))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}

func TestHttpFS(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv := setupHTTP(t)

	base, _ := url.Parse(srv.URL)

	fsys := HTTPFS(base)
	fsys = WithContextFS(ctx, fsys)

	f, err := fsys.Open("hello.txt")
	assert.NoError(t, err)

	defer f.Close()

	body, err := io.ReadAll(f)
	assert.NoError(t, err)
	assert.Equal(t, "hello world", string(body))

	body, err = fs.ReadFile(fsys, "sub/subfile.json")
	assert.NoError(t, err)
	assert.Equal(t, `{"msg": "hi there"}`, string(body))

	hdr := http.Header{}
	hdr.Set("Accept", "application/json")
	fi, err := fs.Stat(WithHeaderFS(hdr, fsys), "sub/subfile.json")
	assert.NoError(t, err)
	assert.Equal(t, "application/json", ContentType(fi))

	fi, err = fs.Stat(fsys, "hello.txt")
	assert.NoError(t, err)
	assert.Equal(t, int64(11), fi.Size())
	assert.Equal(t, "hello.txt", fi.Name())
	assert.Equal(t, "text/plain", ContentType(fi))

	lmod, _ := time.Parse(time.RFC3339, "2021-04-01T12:00:00Z")
	assert.Equal(t, lmod, fi.ModTime())

	assert.False(t, fi.IsDir())
	assert.Nil(t, fi.Sys())

	_, err = fs.Stat(fsys, "bogus")
	assert.Error(t, err)
}

func setupExampleHTTPServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lmod, _ := time.Parse(time.RFC3339, "2021-04-01T12:00:00Z")
		w.Header().Set("Last-Modified", lmod.Format(http.TimeFormat))
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("hello, world!"))
	}))
}

func ExampleHTTPFS() {
	srv := setupExampleHTTPServer()
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	base, _ := url.Parse(srv.URL)

	fsys := HTTPFS(base)
	fsys = WithContextFS(ctx, fsys)

	b, _ := fs.ReadFile(fsys, "hello.txt")
	fmt.Printf("%s", string(b))
	// Output:
	// hello, world!
}
