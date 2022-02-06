package fsimpl

import (
	"io/fs"
	"net/url"
	"os"
	"testing"
	"testing/fstest"

	"github.com/hairyhenderson/go-fsimpl/internal/tests"
	"github.com/stretchr/testify/assert"
)

func TestFSMux(t *testing.T) {
	fsys := os.DirFS("/tmp")
	fn := func(u *url.URL) (fs.FS, error) { return fsys, nil }
	fsp := FSProviderFunc(fn, "foo", "bar")
	fsp2 := FSProviderFunc(fn, "baz", "qux")

	m := NewMux()

	_, err := m.Lookup(":bogus/url")
	assert.Error(t, err)

	_, err = m.Lookup("foo:///")
	assert.Error(t, err)

	m.Add(fsp)
	m.Add(fsp2)

	actual, err := m.Lookup("foo:///")
	assert.NoError(t, err)
	assert.Equal(t, fsys, actual)

	actual, err = m.Lookup("bar:///")
	assert.NoError(t, err)
	assert.Equal(t, fsys, actual)

	actual, err = m.Lookup("qux:///")
	assert.NoError(t, err)
	assert.Equal(t, fsys, actual)

	_, err = m.Lookup("file:///")
	assert.Error(t, err)
}

func TestWrappedFSProvider(t *testing.T) {
	basefsys := fstest.MapFS{}
	basefsys["root/sub/subsub/file.txt"] = &fstest.MapFile{Data: []byte("hello")}
	basefsys["root/sub/subsub/file2.txt"] = &fstest.MapFile{Data: []byte("world")}
	basefsys["rootfile.txt"] = &fstest.MapFile{Data: []byte("rootfile")}

	fsp := WrappedFSProvider(&basefsys, "foo")
	fsys, err := fsp.New(tests.MustURL("foo:///"))
	assert.NoError(t, err)
	assert.Same(t, &basefsys, fsys)

	fsys, err = fsp.New(tests.MustURL("foo:///root/sub"))
	assert.NoError(t, err)
	assert.NotSame(t, &basefsys, fsys)

	b, err := fs.ReadFile(fsys, "subsub/file.txt")
	assert.NoError(t, err)
	assert.Equal(t, []byte("hello"), b)
}
