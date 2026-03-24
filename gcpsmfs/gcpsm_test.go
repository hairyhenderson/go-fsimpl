package gcpsmfs

import (
	"io"
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

func TestOpen(t *testing.T) {
	mc := &mockClient{
		secrets: map[string][]byte{
			"projects/p/secrets/foo/versions/latest": []byte("bar"),
		},
	}

	t.Run("simple name with project in URL succeeds", func(t *testing.T) {
		u, _ := url.Parse("gcp+sm:///projects/p")
		fsys, _ := New(u)
		fsys = WithSMClientFS(mc, fsys)

		f, err := fsys.Open("foo")
		require.NoError(t, err)
		require.NotNil(t, f)
		_ = f.Close()
	})

	t.Run("slash in file name returns invalid", func(t *testing.T) {
		u, _ := url.Parse("gcp+sm:///projects/p")
		fsys, _ := New(u)
		fsys = WithSMClientFS(mc, fsys)

		_, err := fsys.Open("foo/bar")
		require.Error(t, err)
		assert.ErrorIs(t, err, fs.ErrInvalid)
	})
}

func TestReadFile(t *testing.T) {
	mc := &mockClient{
		secrets: map[string][]byte{
			"projects/p/secrets/foo/versions/latest": []byte("bar"),
		},
	}

	t.Run("full path with no project in URL succeeds", func(t *testing.T) {
		u, _ := url.Parse("gcp+sm://")
		fsys, _ := New(u)
		fsys = WithSMClientFS(mc, fsys)

		b, err := fs.ReadFile(fsys, "projects/p/secrets/foo")
		require.NoError(t, err)
		assert.Equal(t, []byte("bar"), b)
	})

	t.Run("missing secret returns not exist", func(t *testing.T) {
		u, _ := url.Parse("gcp+sm://")
		fsys, _ := New(u)
		fsys = WithSMClientFS(mc, fsys)

		_, err := fs.ReadFile(fsys, "projects/p/secrets/baz")
		assert.ErrorIs(t, err, fs.ErrNotExist)
	})

	t.Run("slash in file name returns invalid", func(t *testing.T) {
		u, _ := url.Parse("gcp+sm:///projects/p")
		fsys, _ := New(u)
		fsys = WithSMClientFS(mc, fsys)

		_, err := fs.ReadFile(fsys, "foo/bar")
		require.Error(t, err)
		assert.ErrorIs(t, err, fs.ErrInvalid)
	})
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

	t.Run("without read", func(t *testing.T) {
		fi, err := fs.Stat(fsys, "foo")
		require.NoError(t, err)
		assert.Equal(t, "foo", fi.Name())
		assert.Equal(t, int64(3), fi.Size())
		assert.False(t, fi.IsDir())
		assert.Equal(t, testSecretVersionModTime(), fi.ModTime().UTC())

		fi, err = fs.Stat(fsys, ".")
		require.NoError(t, err)
		assert.Equal(t, ".", fi.Name())
		assert.True(t, fi.IsDir())
	})

	t.Run("after read", func(t *testing.T) {
		f, err := fsys.Open("foo")
		require.NoError(t, err)
		t.Cleanup(func() { _ = f.Close() })

		_, err = io.ReadAll(f)
		require.NoError(t, err)

		fi, err := f.Stat()
		require.NoError(t, err)
		assert.Equal(t, testSecretVersionModTime(), fi.ModTime().UTC())
	})
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

// TestEmptyProject_Open verifies Open behavior when the FS has no project in the URL.
func TestEmptyProject_Open(t *testing.T) {
	mc := &mockClient{
		secrets: map[string][]byte{
			"projects/myproj/secrets/foo/versions/latest": []byte("secret-data"),
		},
	}

	u, _ := url.Parse("gcp+sm:///")
	fsys, _ := New(u)
	fsys = WithSMClientFS(mc, fsys)

	t.Run("open root with empty project returns invalid path", func(t *testing.T) {
		_, err := fsys.Open(".")
		require.Error(t, err)
		assert.ErrorIs(t, err, fs.ErrInvalid)
	})

	t.Run("open with full path succeeds", func(t *testing.T) {
		file, err := fsys.Open("projects/myproj/secrets/foo")
		require.NoError(t, err)
		require.NotNil(t, file)
		_ = file.Close()
	})

	t.Run("open with short path returns invalid", func(t *testing.T) {
		_, err := fsys.Open("foo")
		require.Error(t, err)
		assert.ErrorIs(t, err, fs.ErrInvalid)
	})
}

// TestEmptyProject_ReadFile verifies ReadFile behavior when the FS has no project in the URL.
func TestEmptyProject_ReadFile(t *testing.T) {
	mc := &mockClient{
		secrets: map[string][]byte{
			"projects/myproj/secrets/foo/versions/latest": []byte("secret-data"),
		},
	}

	u, _ := url.Parse("gcp+sm:///")
	fsys, _ := New(u)
	fsys = WithSMClientFS(mc, fsys)

	t.Run("readfile with full path succeeds", func(t *testing.T) {
		b, err := fs.ReadFile(fsys, "projects/myproj/secrets/foo")
		require.NoError(t, err)
		assert.Equal(t, []byte("secret-data"), b)
	})

	t.Run("readfile with relative path returns invalid", func(t *testing.T) {
		_, err := fs.ReadFile(fsys, "foo")
		require.Error(t, err)
		assert.ErrorIs(t, err, fs.ErrInvalid)
	})
}

// TestEmptyProject_ReadDir verifies ReadDir behavior when the FS has no project in the URL.
func TestEmptyProject_ReadDir(t *testing.T) {
	mc := &mockClient{
		secrets: map[string][]byte{
			"projects/p/secrets/foo/versions/latest": []byte("bar"),
		},
	}

	u, _ := url.Parse("gcp+sm:///")
	fsys, _ := New(u)
	fsys = WithSMClientFS(mc, fsys)

	t.Run("readdir requires project in URL", func(t *testing.T) {
		_, err := fs.ReadDir(fsys, ".")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "listing secrets requires a project in the URL")
	})

	t.Run("readdir on non-root returns not exist", func(t *testing.T) {
		_, err := fs.ReadDir(fsys, "projects/p/secrets")
		require.Error(t, err)
		assert.ErrorIs(t, err, fs.ErrNotExist)
	})
}
