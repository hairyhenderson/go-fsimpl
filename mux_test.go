package fsimpl

import (
	"io/fs"
	"net/url"
	"os"
	"testing"
	"testing/fstest"

	"github.com/hairyhenderson/go-fsimpl/internal/tests"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFSMux(t *testing.T) {
	fsys := os.DirFS("/tmp")
	fn := func(_ *url.URL) (fs.FS, error) { return fsys, nil }
	fsp := FSProviderFunc(fn, "foo", "bar")
	fsp2 := FSProviderFunc(fn, "baz", "qux")

	m := NewMux()

	_, err := m.Lookup(":bogus/url")
	require.Error(t, err)

	_, err = m.Lookup("foo:///")
	require.Error(t, err)

	m.Add(fsp)
	m.Add(fsp2)

	actual, err := m.Lookup("foo:///")
	require.NoError(t, err)
	assert.Equal(t, fsys, actual)

	actual, err = m.Lookup("bar:///")
	require.NoError(t, err)
	assert.Equal(t, fsys, actual)

	actual, err = m.Lookup("qux:///")
	require.NoError(t, err)
	assert.Equal(t, fsys, actual)

	_, err = m.Lookup("file:///")
	require.Error(t, err)

	// test out FSProvider functionality
	assert.Equal(t, []string{"bar", "baz", "foo", "qux"}, m.Schemes())
	actual, err = m.New(tests.MustURL("foo:///"))
	require.NoError(t, err)
	assert.Equal(t, fsys, actual)
	actual, err = m.New(tests.MustURL("bar:///"))
	require.NoError(t, err)
	assert.Equal(t, fsys, actual)
}

func TestWrappedFSProvider(t *testing.T) {
	basefsys := fstest.MapFS{}
	basefsys["root/sub/subsub/file.txt"] = &fstest.MapFile{Data: []byte("hello")}
	basefsys["root/sub/subsub/file2.txt"] = &fstest.MapFile{Data: []byte("world")}
	basefsys["rootfile.txt"] = &fstest.MapFile{Data: []byte("rootfile")}

	fsp := WrappedFSProvider(&basefsys, "foo")
	fsys, err := fsp.New(tests.MustURL("foo:///"))
	require.NoError(t, err)
	assert.Same(t, &basefsys, fsys)

	fsys, err = fsp.New(tests.MustURL("foo:///root/sub"))
	require.NoError(t, err)
	assert.NotSame(t, &basefsys, fsys)

	b, err := fs.ReadFile(fsys, "subsub/file.txt")
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), b)
}
