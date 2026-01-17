package consulfs

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"runtime"
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

func TestGetAddress(t *testing.T) {
	assert.Empty(t, getAddress(tests.MustURL("consul:///")))
	assert.Equal(t, "http://myconsul.local:1234",
		getAddress(tests.MustURL("consul://myconsul.local:1234")))
	assert.Equal(t, "https://consul.example.com",
		getAddress(tests.MustURL("consul+https://consul.example.com")))
}

func TestNew(t *testing.T) {
	_, err := New(nil)
	require.Error(t, err)

	_, err = New(tests.MustURL("consul:///secret/foo"))
	require.Error(t, err)

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
		require.NoError(t, err)
		require.IsType(t, &consulFS{}, fsys)

		consulfs := fsys.(*consulFS)
		assert.Equal(t, d.expected, consulfs.base.String())
	}
}

func TestWithContext(t *testing.T) {
	type key struct{}

	ctx := context.WithValue(t.Context(), key{}, "foo")

	fsys := &consulFS{ctx: t.Context()}
	fsys = fsys.WithContext(ctx).(*consulFS)

	assert.Same(t, ctx, fsys.ctx)
}

func TestWithHeader(t *testing.T) {
	fsys := &consulFS{config: fakeConsulServer(t), base: tests.MustURL("consul:///")}

	hdr := http.Header{}
	hdr.Add("foo", "bar")
	fsys = fsys.WithHeader(hdr).(*consulFS)

	err := fsys.initClient()
	require.NoError(t, err)

	require.Equal(t, "bar", fsys.client.Headers().Get("foo"))

	fsys = &consulFS{config: fakeConsulServer(t), base: tests.MustURL("consul:///")}

	err = fsys.initClient()
	require.NoError(t, err)

	fsys.client.AddHeader("foo", "bar")

	hdr = http.Header{}
	hdr.Add("foo", "bar2")
	hdr.Add("baz", "qux")
	fsys = fsys.WithHeader(hdr).(*consulFS)

	assert.Equal(t, []string{"bar", "bar2"}, fsys.client.Headers().Values("foo"))
	assert.Equal(t, "qux", fsys.client.Headers().Get("baz"))
}

func TestWithToken(t *testing.T) {
	fsys := &consulFS{client: &api.Client{}}
	fsys = fsys.WithToken("foo").(*consulFS)

	assert.Equal(t, "foo", fsys.token)

	// WithToken should clear the client
	assert.Nil(t, fsys.client)
}

func TestWithQueryOptions(t *testing.T) {
	opts := &api.QueryOptions{}

	fsys := &consulFS{client: &api.Client{}}
	fsys = fsys.WithQueryOptions(opts).(*consulFS)

	assert.Same(t, opts, fsys.queryOpts)
	// WithQueryOptions must not clear the client
	assert.NotNil(t, fsys.client)
}

func TestOpen(t *testing.T) {
	fsys, err := New(tests.MustURL("consul+https://127.0.0.1:8500/foo/"))
	require.NoError(t, err)

	_, err = fsys.Open("/bogus")
	require.Error(t, err)

	if runtime.GOOS != "windows" {
		_, err = fsys.Open("bo\\gus")
		require.Error(t, err)
	}
}

func TestReadFile(t *testing.T) {
	expected := "foo"
	config := fakeConsulServer(t)

	fsys, err := New(tests.MustURL("consul:///dir/"))
	require.NoError(t, err)

	fsys = WithConfigFS(config, fsys)

	f, err := fsys.Open("foo")
	require.NoError(t, err)

	b, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, []byte(expected), b)

	b, err = fs.ReadFile(fsys, "bar")
	require.NoError(t, err)
	assert.Equal(t, []byte(expected), b)

	require.NoError(t, f.Close())

	// should error on second call
	require.Error(t, f.Close())
}

func TestReadDirFS(t *testing.T) {
	config := fakeConsulServer(t)

	fsys, err := New(tests.MustURL("consul:///dir/sub/"))
	require.NoError(t, err)

	fsys = WithConfigFS(config, fsys)

	de, err := fs.ReadDir(fsys, ".")
	require.NoError(t, err)

	des := []fs.DirEntry{
		internal.FileInfo("bar", 3, 0o444, time.Time{}, "").(fs.DirEntry),
		internal.DirInfo("bazDir", time.Time{}).(fs.DirEntry),
		internal.FileInfo("foo", 3, 0o444, time.Time{}, "").(fs.DirEntry),
	}
	assert.Equal(t, des, de)

	fsys, err = New(tests.MustURL("consul:///dir/"))
	require.NoError(t, err)

	fsys = WithConfigFS(config, fsys)

	de, err = fs.ReadDir(fsys, "sub")
	require.NoError(t, err)

	des = []fs.DirEntry{
		internal.FileInfo("bar", 3, 0o444, time.Time{}, "").(fs.DirEntry),
		internal.DirInfo("bazDir", time.Time{}).(fs.DirEntry),
		internal.FileInfo("foo", 3, 0o444, time.Time{}, "").(fs.DirEntry),
	}
	assert.Equal(t, des, de)
}

//nolint:funlen
func TestReadDirN(t *testing.T) {
	config := fakeConsulServer(t)

	fsys, err := New(tests.MustURL("consul:///dir/"))
	require.NoError(t, err)

	fsys = WithConfigFS(config, fsys)

	// open and read a few entries at a time
	df, err := fsys.Open("sub")
	require.NoError(t, err)
	assert.Implements(t, (*fs.ReadDirFile)(nil), df)

	defer df.Close()

	dir := df.(fs.ReadDirFile)
	de, err := dir.ReadDir(1)
	require.NoError(t, err)

	des := []fs.DirEntry{
		internal.FileInfo("bar", 3, 0o444, time.Time{}, "").(fs.DirEntry),
	}
	assert.Equal(t, des, de)

	de, err = dir.ReadDir(2)
	require.NoError(t, err)

	des = []fs.DirEntry{
		internal.DirInfo("bazDir", time.Time{}).(fs.DirEntry),
		internal.FileInfo("foo", 3, 0o444, time.Time{}, "").(fs.DirEntry),
	}
	assert.Equal(t, des, de)

	de, err = dir.ReadDir(1)
	require.ErrorIs(t, err, io.EOF)
	assert.Empty(t, de)

	// open and read everything
	df, err = fsys.Open("sub")
	require.NoError(t, err)

	defer df.Close()

	dir = df.(fs.ReadDirFile)
	de, err = dir.ReadDir(0)
	require.NoError(t, err)
	assert.Len(t, de, 3)

	// open and read everything a few times
	df, err = fsys.Open("sub")
	require.NoError(t, err)

	defer df.Close()

	dir = df.(fs.ReadDirFile)
	de, err = dir.ReadDir(-1)
	require.NoError(t, err)
	assert.Len(t, de, 3)

	de, err = dir.ReadDir(-1)
	require.NoError(t, err)
	assert.Empty(t, de)

	// open and read too many entries
	df, err = fsys.Open(".")
	require.NoError(t, err)

	defer df.Close()

	dir = df.(fs.ReadDirFile)
	de, err = dir.ReadDir(8)
	require.ErrorIs(t, err, io.EOF)
	assert.Len(t, de, 3)
}

func TestStat(t *testing.T) {
	config := fakeConsulServer(t)

	fsys, err := New(tests.MustURL("consul:///dir/"))
	require.NoError(t, err)

	fsys = WithConfigFS(config, fsys)

	f, err := fsys.Open("foo")
	require.NoError(t, err)

	fi, err := f.Stat()
	require.NoError(t, err)
	assert.Empty(t, fsimpl.ContentType(fi))

	err = f.Close()
	require.NoError(t, err)

	f, err = fsys.Open("bogus")
	require.NoError(t, err)

	_, err = f.Stat()
	require.ErrorIs(t, err, fs.ErrNotExist)

	err = f.Close()
	require.NoError(t, err)

	fi, err = fs.Stat(fsys, "sub")
	require.NoError(t, err)
	assert.Equal(t, internal.DirInfo("sub", time.Time{}), fi)

	fi, err = fs.Stat(fsys, ".")
	require.NoError(t, err)
	assert.Equal(t, internal.DirInfo(".", time.Time{}), fi)
}

func TestRootLevelOperations(t *testing.T) {
	config := fakeConsulServer(t)

	fsys, err := New(tests.MustURL("consul:///"))
	require.NoError(t, err)

	fsys = WithConfigFS(config, fsys)

	// Stat root directory
	fi, err := fs.Stat(fsys, ".")
	require.NoError(t, err)
	assert.True(t, fi.IsDir())
	assert.Equal(t, ".", fi.Name())

	// List root directory
	entries, err := fs.ReadDir(fsys, ".")
	require.NoError(t, err)
	assert.Len(t, entries, 3)

	// Verify entries are sorted: bar, dir, foo
	names := []string{entries[0].Name(), entries[1].Name(), entries[2].Name()}
	assert.Equal(t, []string{"bar", "dir", "foo"}, names)

	// Read a root-level file
	content, err := fs.ReadFile(fsys, "foo")
	require.NoError(t, err)
	assert.Equal(t, "foo value", string(content))
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
