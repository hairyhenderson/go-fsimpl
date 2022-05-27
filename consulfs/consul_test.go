package consulfs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/hairyhenderson/go-fsimpl"
	"github.com/hairyhenderson/go-fsimpl/internal"
	"github.com/hairyhenderson/go-fsimpl/internal/tests"
	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Example() {
	base, _ := url.Parse("consul://my.consulserver.local:8500")

	fsys, _ := New(base)

	b, _ := fs.ReadFile(fsys, "mykey")

	fmt.Printf("the secret is %s\n", string(b))
}

func fakeConsul(t *testing.T, handler http.Handler) *api.Client {
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	tr := &http.Transport{
		Proxy: func(req *http.Request) (*url.URL, error) {
			return url.Parse(srv.URL)
		},
	}
	httpClient := &http.Client{Transport: tr}
	config := &api.Config{Address: srv.URL, HttpClient: httpClient}

	c, _ := api.NewClient(config)

	return c
}

//nolint:funlen
func fakeConsulServer(t *testing.T) *api.Client {
	t.Helper()

	files := map[string]struct {
		Value string   `json:"value,omitempty"`
		Keys  []string `json:"keys,omitempty"`
	}{
		"/v1/kv/dir/":               {Keys: []string{"dir/foo", "dir/bar", "dir/sub/"}},
		"/v1/kv/dir/foo":            {Value: "foo"},
		"/v1/kv/dir/bar":            {Value: "foo"},
		"/v1/kv/dir/sub/":           {Keys: []string{"dir/sub/foo", "dir/sub/bar", "dir/sub/bazDir/"}},
		"/v1/kv/dir/sub/foo":        {Value: "foo"},
		"/v1/kv/dir/sub/bar":        {Value: "foo"},
		"/v1/kv/dir/sub/bazDir/":    {Keys: []string{"dir/sub/bazDir/qux"}},
		"/v1/kv/dir/sub/bazDir/qux": {Value: "qux"},
	}

	return fakeConsul(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Helper()

		t.Logf("req to path %+v", r.URL.Path)

		data, ok := files[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)

		q := r.URL.Query()
		if q.Has("keys") {
			t.Logf("returning keys %+v", data.Keys)

			_ = enc.Encode(data.Keys)

			return
		}

		pairs := []*api.KVPair{}
		if !q.Has("recurse") {
			pairs = []*api.KVPair{
				{
					Key:   r.URL.Path[len("/v1/kv/"):],
					Value: []byte(data.Value),
				},
			}

			assert.NotEmpty(t, data.Value, r.URL)
		} else {
			for k, v := range files {
				if k == r.URL.Path {
					continue
				}

				if strings.HasPrefix(k, r.URL.Path) {
					pairs = append(pairs, &api.KVPair{
						Key:   k[len("/v1/kv/"):],
						Value: []byte(v.Value),
					})
				}
			}

			sort.Slice(pairs, func(i, j int) bool {
				return pairs[i].Key < pairs[j].Key
			})
		}

		t.Logf("returning pairs %+v", pairs)

		_ = enc.Encode(pairs)
	}))
}

func TestConsulConfig(t *testing.T) {
	config := defaultConsulConfig(tests.MustURL("consul:///"))
	assert.Equal(t, "127.0.0.1:8500", config.Address)

	config = defaultConsulConfig(tests.MustURL("consul://myconsul.local:1234"))
	assert.Equal(t, "http://myconsul.local:1234", config.Address)

	config = defaultConsulConfig(tests.MustURL("consul+https://consul.example.com"))
	assert.Equal(t, "https://consul.example.com", config.Address)
}

func TestNew(t *testing.T) {
	_, err := New(nil)
	assert.Error(t, err)

	_, err = New(tests.MustURL("consul:///secret/foo"))
	assert.Error(t, err)

	testdata := []struct {
		in, expected string
	}{
		{"consul:///", "consul:///"},
		{"consul+https://example.com", "consul+https://example.com/"},
		{"consul:///?param=value", "consul:///?param=value"},
		{"consul:///secret/?param=value", "consul:///secret/?param=value"},
		{"consul:///secret/?param=value", "consul:///secret/?param=value"},
	}

	for _, d := range testdata {
		fsys, err := New(tests.MustURL(d.in))
		assert.NoError(t, err)
		require.IsType(t, &consulFS{}, fsys)

		consulfs := fsys.(*consulFS)
		assert.Equal(t, d.expected, consulfs.base.String())
	}
}

func TestWithContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), struct{}{}, "foo")

	fsys := &consulFS{ctx: context.Background()}
	fsys = fsys.WithContext(ctx).(*consulFS)

	assert.Same(t, ctx, fsys.ctx)
}

func TestWithHeader(t *testing.T) {
	fsys := &consulFS{client: fakeConsulServer(t)}

	fsys = fsys.WithHeader(http.Header{
		"foo": []string{"bar"},
	}).(*consulFS)

	assert.Equal(t, "bar", fsys.client.Headers().Get("foo"))

	fsys = &consulFS{client: fakeConsulServer(t)}
	fsys.client.AddHeader("foo", "bar")

	fsys = fsys.WithHeader(http.Header{
		"foo": []string{"bar2"},
		"baz": []string{"qux"},
	}).(*consulFS)

	assert.EqualValues(t, []string{"bar", "bar2"}, fsys.client.Headers().Values("foo"))
	assert.EqualValues(t, "qux", fsys.client.Headers().Get("baz"))
}

func TestOpen(t *testing.T) {
	fsys, err := New(tests.MustURL("consul+https://127.0.0.1:8500/foo/"))
	assert.NoError(t, err)

	_, err = fsys.Open("/bogus")
	assert.Error(t, err)

	if runtime.GOOS != "windows" {
		_, err = fsys.Open("bo\\gus")
		assert.Error(t, err)
	}
}

func TestReadFile(t *testing.T) {
	expected := "foo"
	client := fakeConsulServer(t)

	fsys := fs.FS(newWithConsulClient(tests.MustURL("consul:///dir/"), client))

	f, err := fsys.Open("foo")
	assert.NoError(t, err)

	b, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, []byte(expected), b)

	b, err = fs.ReadFile(fsys, "bar")
	assert.NoError(t, err)
	assert.Equal(t, []byte(expected), b)

	err = f.Close()
	assert.NoError(t, err)
}

func TestReadDirFS(t *testing.T) {
	client := fakeConsulServer(t)

	fsys := fs.FS(newWithConsulClient(tests.MustURL("consul:///dir/sub/"), client))

	de, err := fs.ReadDir(fsys, ".")
	assert.NoError(t, err)

	des := []fs.DirEntry{
		internal.FileInfo("bar", 3, 0o644, time.Time{}, "").(fs.DirEntry),
		internal.DirInfo("bazDir", time.Time{}).(fs.DirEntry),
		internal.FileInfo("foo", 3, 0o644, time.Time{}, "").(fs.DirEntry),
	}
	assert.EqualValues(t, des, de)

	fsys = newWithConsulClient(tests.MustURL("consul:///dir/"), client)

	de, err = fs.ReadDir(fsys, "sub")
	assert.NoError(t, err)

	des = []fs.DirEntry{
		internal.FileInfo("bar", 3, 0o644, time.Time{}, "").(fs.DirEntry),
		internal.DirInfo("bazDir", time.Time{}).(fs.DirEntry),
		internal.FileInfo("foo", 3, 0o644, time.Time{}, "").(fs.DirEntry),
	}
	assert.EqualValues(t, des, de)
}

//nolint:funlen
func TestReadDirN(t *testing.T) {
	client := fakeConsulServer(t)

	fsys := fs.FS(newWithConsulClient(tests.MustURL("consul:///dir/"), client))

	// open and read a few entries at a time
	df, err := fsys.Open("sub")
	assert.NoError(t, err)
	assert.Implements(t, (*fs.ReadDirFile)(nil), df)

	defer df.Close()

	dir := df.(fs.ReadDirFile)
	de, err := dir.ReadDir(1)
	assert.NoError(t, err)

	des := []fs.DirEntry{
		internal.FileInfo("bar", 3, 0o644, time.Time{}, "").(fs.DirEntry),
	}
	assert.EqualValues(t, des, de)

	de, err = dir.ReadDir(2)
	assert.NoError(t, err)

	des = []fs.DirEntry{
		internal.DirInfo("bazDir", time.Time{}).(fs.DirEntry),
		internal.FileInfo("foo", 3, 0o644, time.Time{}, "").(fs.DirEntry),
	}
	assert.EqualValues(t, des, de)

	de, err = dir.ReadDir(1)
	assert.ErrorIs(t, err, io.EOF)
	assert.Len(t, de, 0)

	// open and read everything
	df, err = fsys.Open("sub")
	assert.NoError(t, err)

	defer df.Close()

	dir = df.(fs.ReadDirFile)
	de, err = dir.ReadDir(0)
	assert.NoError(t, err)
	assert.Len(t, de, 3)

	// open and read everything a few times
	df, err = fsys.Open("sub")
	assert.NoError(t, err)

	defer df.Close()

	dir = df.(fs.ReadDirFile)
	de, err = dir.ReadDir(-1)
	assert.NoError(t, err)
	assert.Len(t, de, 3)

	de, err = dir.ReadDir(-1)
	assert.NoError(t, err)
	assert.Len(t, de, 0)

	// open and read too many entries
	df, err = fsys.Open(".")
	assert.NoError(t, err)

	defer df.Close()

	dir = df.(fs.ReadDirFile)
	de, err = dir.ReadDir(8)
	assert.ErrorIs(t, err, io.EOF)
	assert.Len(t, de, 3)
}

func TestSubURL(t *testing.T) {
	base := tests.MustURL("https://example.com/dir/")
	sub, err := subURL(base, "sub")
	assert.NoError(t, err)
	assert.Equal(t, "https://example.com/dir/sub", sub.String())

	base = tests.MustURL("consul:///dir/")
	sub, err = subURL(base, "sub/foo?param=foo")
	assert.NoError(t, err)
	assert.Equal(t, "consul:///dir/sub/foo?param=foo", sub.String())
}

func TestStat(t *testing.T) {
	client := fakeConsulServer(t)

	fsys := newWithConsulClient(tests.MustURL("consul:///dir/"), client)

	f, err := fsys.Open("foo")
	assert.NoError(t, err)

	fi, err := f.Stat()
	require.NoError(t, err)
	assert.Equal(t, "", fsimpl.ContentType(fi))

	err = f.Close()
	assert.NoError(t, err)

	f, err = fsys.Open("bogus")
	assert.NoError(t, err)

	_, err = f.Stat()
	assert.ErrorIs(t, err, fs.ErrNotExist)

	err = f.Close()
	assert.NoError(t, err)

	fi, err = fs.Stat(fsys, "sub")
	assert.NoError(t, err)
	assert.EqualValues(t, internal.DirInfo("sub", time.Time{}), fi)
}

func TestOnlyChildren(t *testing.T) {
	assert.Equal(t, []string{}, onlyChildren("", nil))
	assert.Equal(t, []string{"foo"}, onlyChildren("", []string{"foo"}))

	assert.Equal(t, []string{
		"dir/bar", "dir/baz", "dir/foo", "dir/qux",
	}, onlyChildren("dir/", []string{
		"dir/foo", "dir/bar", "dir/baz", "dir/qux",
	}))

	assert.Equal(t, []string{
		"dir/1", "dir/2", "dir/3", "dir/4/",
	}, onlyChildren("dir/", []string{
		"dir/1", "dir/2", "dir/3",
		"dir/4/4.1", "dir/4/4.2", "dir/4/4.3",
	}))
}
