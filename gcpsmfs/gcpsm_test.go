package gcpsmfs

import (
	"io/fs"
	"net/url"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	u, _ := url.Parse("gcp+sm:///projects/my-project")
	fsys, err := New(u)
	require.NoError(t, err)
	assert.NotNil(t, fsys)

	// Invalid scheme
	u, _ = url.Parse("http://foo.com")
	_, err = New(u)
	require.Error(t, err)

	// Missing project
	u, _ = url.Parse("gcp+sm://")
	fsys, err = New(u)
	require.NoError(t, err)
	assert.NotNil(t, fsys)
}

func TestReadFile(t *testing.T) {
	mc := &mockClient{
		secrets: map[string][]byte{
			"projects/p/secrets/foo/versions/latest": []byte("bar"),
		},
	}

	u, _ := url.Parse("gcp+sm://")
	fsys, _ := New(u)
	fsys = WithSMClientFS(mc, fsys)

	b, err := fs.ReadFile(fsys, "projects/p/secrets/foo")
	require.NoError(t, err)
	assert.Equal(t, []byte("bar"), b)

	_, err = fs.ReadFile(fsys, "projects/p/secrets/baz")
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

func TestReadDir(t *testing.T) {
	mc := &mockClient{
		secrets: map[string][]byte{
			"projects/p/secrets/foo/versions/latest": []byte("bar"),
			"projects/p/secrets/baz/versions/latest": []byte("qux"),
		},
	}

	u, _ := url.Parse("gcp+sm:///projects/p")
	fsys, _ := New(u)
	fsys = WithSMClientFS(mc, fsys)

	entries, err := fs.ReadDir(fsys, ".")
	require.NoError(t, err)
	require.Len(t, entries, 2)

	// Sorted order
	assert.Equal(t, "baz", entries[0].Name())
	assert.Equal(t, "foo", entries[1].Name())
}

func TestStat(t *testing.T) {
	mc := &mockClient{
		secrets: map[string][]byte{
			"projects/p/secrets/foo/versions/latest": []byte("bar"),
		},
	}

	u, _ := url.Parse("gcp+sm:///projects/p")
	fsys, err := New(u)
	require.NoError(t, err)
	assert.NotNil(t, fsys)
	fsys = WithSMClientFS(mc, fsys)

	fi, err := fs.Stat(fsys, "foo")
	require.NoError(t, err)
	assert.Equal(t, "foo", fi.Name())
	assert.Equal(t, int64(3), fi.Size())
	assert.False(t, fi.IsDir())

	fi, err = fs.Stat(fsys, ".")
	require.NoError(t, err)
	assert.Equal(t, ".", fi.Name())
	assert.True(t, fi.IsDir())
}

func TestFS(t *testing.T) {
	mc := &mockClient{
		secrets: map[string][]byte{
			"projects/p/secrets/foo/versions/latest": []byte(""),
			"projects/p/secrets/baz/versions/latest": []byte(""),
		},
	}

	u, _ := url.Parse("gcp+sm:///projects/p")
	fsys, _ := New(u)
	fsys = WithSMClientFS(mc, fsys)

	// Verify that fstest passes for this filesystem
	err := fstest.TestFS(fsys, "foo", "baz")
	assert.NoError(t, err)
}
