package gcpsmfs

import (
	"fmt"
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

func TestWithMaxConcurrencyFS(t *testing.T) {
	mc := &mockClient{
		secrets: map[string][]byte{
			"projects/p/secrets/alpha/versions/latest": []byte("a"),
			"projects/p/secrets/beta/versions/latest":  []byte("b"),
			"projects/p/secrets/gamma/versions/latest": []byte("c"),
		},
	}

	u, _ := url.Parse("gcp+sm:///projects/p")

	for _, concurrency := range []int{1, 2, 5} {
		t.Run(fmt.Sprintf("concurrency=%d", concurrency), func(t *testing.T) {
			fsys, _ := New(u)
			fsys = WithSMClientFS(mc, fsys)
			fsys = WithMaxConcurrencyFS(concurrency, fsys)

			entries, err := fs.ReadDir(fsys, ".")
			require.NoError(t, err)
			require.Len(t, entries, 3)

			// Results must always be sorted regardless of fetch order.
			assert.Equal(t, "alpha", entries[0].Name())
			assert.Equal(t, "beta", entries[1].Name())
			assert.Equal(t, "gamma", entries[2].Name())
		})
	}
}

func TestDefaultMaxConcurrencyEnvVar(t *testing.T) {
	t.Setenv("GCP_SM_MAX_CONCURRENCY", "7")

	u, _ := url.Parse("gcp+sm:///projects/p")
	fsys, err := New(u)
	require.NoError(t, err)

	gcpFS, ok := fsys.(*gcpsmFS)
	require.True(t, ok)
	assert.Equal(t, 7, gcpFS.maxConcurrency)
}

func TestDefaultMaxConcurrencyEnvVar_Invalid(t *testing.T) {
	t.Setenv("GCP_SM_MAX_CONCURRENCY", "notanumber")

	u, _ := url.Parse("gcp+sm:///projects/p")
	fsys, err := New(u)
	require.NoError(t, err)

	gcpFS, ok := fsys.(*gcpsmFS)
	require.True(t, ok)
	assert.Equal(t, 1, gcpFS.maxConcurrency)
}

func TestDefaultCacheEnvVar(t *testing.T) {
	t.Run("unset defaults to enabled", func(t *testing.T) {
		u, _ := url.Parse("gcp+sm:///projects/p")
		fsys, err := New(u)
		require.NoError(t, err)

		gcpFS, ok := fsys.(*gcpsmFS)
		require.True(t, ok)
		assert.NotNil(t, gcpFS.cache)
	})

	t.Run("truthy value disables cache", func(t *testing.T) {
		t.Setenv("GCP_SM_DISABLE_CACHE", "1")

		u, _ := url.Parse("gcp+sm:///projects/p")
		fsys, err := New(u)
		require.NoError(t, err)

		gcpFS, ok := fsys.(*gcpsmFS)
		require.True(t, ok)
		assert.Nil(t, gcpFS.cache)
	})

	t.Run("invalid value defaults to enabled", func(t *testing.T) {
		t.Setenv("GCP_SM_DISABLE_CACHE", "notabool")

		u, _ := url.Parse("gcp+sm:///projects/p")
		fsys, err := New(u)
		require.NoError(t, err)

		gcpFS, ok := fsys.(*gcpsmFS)
		require.True(t, ok)
		assert.NotNil(t, gcpFS.cache)
	})
}

func TestCache_ReadDirDeduplicatesRPCs(t *testing.T) {
	mc := &mockClient{
		secrets: map[string][]byte{
			"projects/p/secrets/foo/versions/latest": []byte("bar"),
			"projects/p/secrets/baz/versions/latest": []byte("qux"),
		},
	}

	u, _ := url.Parse("gcp+sm:///projects/p")
	fsys, _ := New(u)
	fsys = WithSMClientFS(mc, fsys)

	// First listing — populates cache; expect 1 AccessSecretVersion + 1 GetSecretVersion per secret.
	entries, err := fs.ReadDir(fsys, ".")
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Equal(t, int32(2), mc.accessCalls.Load(), "first ReadDir: expected 2 AccessSecretVersion calls")
	assert.Equal(t, int32(2), mc.getCalls.Load(), "first ReadDir: expected 2 GetSecretVersion calls")

	// Second listing — all data is in cache; no additional RPCs should be made.
	entries, err = fs.ReadDir(fsys, ".")
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Equal(t, int32(2), mc.accessCalls.Load(), "second ReadDir: AccessSecretVersion count must not increase")
	assert.Equal(t, int32(2), mc.getCalls.Load(), "second ReadDir: GetSecretVersion count must not increase")
}

func TestCache_OpenAndReadDeduplicatesRPCs(t *testing.T) {
	mc := &mockClient{
		secrets: map[string][]byte{
			"projects/p/secrets/foo/versions/latest": []byte("bar"),
		},
	}

	u, _ := url.Parse("gcp+sm:///projects/p")
	fsys, _ := New(u)
	fsys = WithSMClientFS(mc, fsys)

	// First read — fetches from GCP.
	b, err := fs.ReadFile(fsys, "foo")
	require.NoError(t, err)
	assert.Equal(t, []byte("bar"), b)
	assert.Equal(t, int32(1), mc.accessCalls.Load())

	// Second read — served from cache; no additional RPC.
	b, err = fs.ReadFile(fsys, "foo")
	require.NoError(t, err)
	assert.Equal(t, []byte("bar"), b)
	assert.Equal(t, int32(1), mc.accessCalls.Load(), "second ReadFile: AccessSecretVersion count must not increase")
}

func TestWithCacheFS_Disabled(t *testing.T) {
	mc := &mockClient{
		secrets: map[string][]byte{
			"projects/p/secrets/foo/versions/latest": []byte("bar"),
		},
	}

	u, _ := url.Parse("gcp+sm:///projects/p")
	fsys, _ := New(u)
	fsys = WithSMClientFS(mc, fsys)
	fsys = WithCacheFS(false, fsys)

	gcpFS, ok := fsys.(*gcpsmFS)
	require.True(t, ok)
	assert.Nil(t, gcpFS.cache)

	// First read — fetches from GCP.
	b, err := fs.ReadFile(fsys, "foo")
	require.NoError(t, err)
	assert.Equal(t, []byte("bar"), b)
	assert.Equal(t, int32(1), mc.accessCalls.Load())

	// Second read — cache is disabled, so the RPC is repeated.
	b, err = fs.ReadFile(fsys, "foo")
	require.NoError(t, err)
	assert.Equal(t, []byte("bar"), b)
	assert.Equal(t, int32(2), mc.accessCalls.Load(), "second ReadFile: AccessSecretVersion count should increase with cache disabled")
}

func TestWithCacheFS_ReEnable(t *testing.T) {
	u, _ := url.Parse("gcp+sm:///projects/p")
	fsys, _ := New(u)
	fsys = WithCacheFS(false, fsys)
	fsys = WithCacheFS(true, fsys)

	gcpFS, ok := fsys.(*gcpsmFS)
	require.True(t, ok)
	assert.NotNil(t, gcpFS.cache)
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

func TestReadDir_SkipsDisabledVersionSecrets(t *testing.T) {
	mc := &mockClient{
		secrets: map[string][]byte{
			"projects/p/secrets/foo/versions/latest": []byte("bar"),
		},
		disabledVersionSecrets: []string{"disabledsecret"},
	}

	u, _ := url.Parse("gcp+sm:///projects/p")
	fsys, _ := New(u)
	fsys = WithSMClientFS(mc, fsys)

	entries, err := fs.ReadDir(fsys, ".")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "foo", entries[0].Name())
}

func TestReadDir_SkipsVersionlessSecrets(t *testing.T) {
	mc := &mockClient{
		secrets: map[string][]byte{
			"projects/p/secrets/foo/versions/latest": []byte("bar"),
		},
		noVersionSecrets: []string{"noversionsecret"},
	}

	u, _ := url.Parse("gcp+sm:///projects/p")
	fsys, _ := New(u)
	fsys = WithSMClientFS(mc, fsys)

	entries, err := fs.ReadDir(fsys, ".")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "foo", entries[0].Name())
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

	t.Run("open project-only path returns directory", func(t *testing.T) {
		file, err := fsys.Open("projects/myproj")
		require.NoError(t, err)
		fi, err := file.Stat()
		require.NoError(t, err)
		assert.True(t, fi.IsDir())

		_ = file.Close()
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

	t.Run("readdir root with no project returns descriptive error", func(t *testing.T) {
		_, err := fs.ReadDir(fsys, ".")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires a project")
	})

	t.Run("readdir on non-root returns invalid", func(t *testing.T) {
		_, err := fs.ReadDir(fsys, "projects/p/secrets")
		require.Error(t, err)
		assert.ErrorIs(t, err, fs.ErrInvalid)
	})

	t.Run("readdir project-only path succeeds", func(t *testing.T) {
		entries, err := fs.ReadDir(fsys, "projects/p")
		require.NoError(t, err)
		require.Len(t, entries, 1)
		assert.Equal(t, "foo", entries[0].Name())
	})
}
