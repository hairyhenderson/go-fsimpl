package fsimpl

import (
	"io/fs"
	"net/url"
	"os"
	"testing"

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
