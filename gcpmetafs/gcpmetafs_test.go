package gcpmetafs

import (
	"io"
	"io/fs"
	"net/url"
	"testing"
	"testing/fstest"

	"github.com/hairyhenderson/go-fsimpl/internal"
	"github.com/hairyhenderson/go-fsimpl/internal/tests"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setup(t *testing.T) fs.FS {
	t.Helper()

	// Start the fake metadata server which sets GCE_METADATA_HOST env var
	fakeMetadataServer(t)

	u, err := url.Parse("gcp+meta:///")
	if err != nil {
		t.Fatal(err)
	}

	fsys, err := New(u)
	if err != nil {
		t.Fatal(err)
	}

	fsys = fsys.(internal.WithContexter).WithContext(t.Context())

	return fsys
}

func TestGCPMetaFS_TestFS(t *testing.T) {
	fsys := setup(t)

	require.NoError(t, fstest.TestFS(fsys, "instance", "project"))
}

func TestGCPMetaFS_New(t *testing.T) {
	_, err := New(tests.MustURL("https://example.com"))
	require.Error(t, err)
}

func TestGCPMetaFS_Stat(t *testing.T) {
	fsys := setup(t)

	f, err := fsys.Open("instance")
	require.NoError(t, err)

	fi, err := f.Stat()
	require.NoError(t, err)
	assert.True(t, fi.IsDir())
	assert.Equal(t, "instance", fi.Name())

	f, err = fsys.Open("instance/foo")
	require.NoError(t, err)

	_, err = f.Stat()
	require.Error(t, err)
	require.ErrorIs(t, err, fs.ErrNotExist)

	_, err = fs.Stat(fsys, "noleadingslash")
	require.Error(t, err)
	require.ErrorIs(t, err, fs.ErrNotExist)

	fi, err = fs.Stat(fsys, "instance")
	require.NoError(t, err)
	assert.True(t, fi.IsDir())
}

func TestGCPMetaFS_Read(t *testing.T) {
	fsys := setup(t)

	f, err := fsys.Open("instance/id")
	require.NoError(t, err)

	b, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, "1234567890123456789", string(b))

	fsys, err = fs.Sub(fsys, "instance/network-interfaces")
	require.NoError(t, err)

	f, err = fsys.Open("0/ip")
	require.NoError(t, err)

	defer f.Close()

	b, err = io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.2", string(b))

	_, err = f.Read(b)
	require.Error(t, err)
}

func TestGCPMetaFS_ReadFile(t *testing.T) {
	fsys := setup(t)

	b, err := fs.ReadFile(fsys, "instance/hostname")
	require.NoError(t, err)
	assert.Equal(t, "instance-1.c.project-id.internal", string(b))

	// should error because this is a directory
	_, err = fs.ReadFile(fsys, "instance")
	require.ErrorIs(t, err, errIsDirectory)

	subfsys, err := fs.Sub(fsys, "project")
	require.NoError(t, err)

	b, err = fs.ReadFile(subfsys, "project-id")
	require.NoError(t, err)
	assert.Equal(t, "test-project-id", string(b))
}

func TestGCPMetaFS_ReadDir(t *testing.T) {
	fsys := setup(t)

	de, err := fs.ReadDir(fsys, ".")
	require.NoError(t, err)
	assert.Len(t, de, 2)

	for i, ex := range []string{"instance", "project"} {
		assert.Equal(t, ex, de[i].Name())
		assert.True(t, de[i].IsDir())
	}

	// Now test reading the instance directory which should have 10 entries
	de, err = fs.ReadDir(fsys, "instance")
	require.NoError(t, err)
	assert.Len(t, de, 10)

	testdata := []struct {
		name string
		dir  bool
	}{
		{"attributes", true},
		{"cpu-platform", false},
		{"disks", true},
		{"hostname", false},
		{"id", false},
		{"image", false},
		{"machine-type", false},
		{"network-interfaces", true},
		{"service-accounts", true},
		{"zone", false},
	}
	for i, d := range testdata {
		var fi fs.FileInfo
		fi, err = de[i].Info()
		require.NoError(t, err)
		assert.Equal(t, d.name, fi.Name())
		assert.Equal(t, d.dir, fi.IsDir())
	}

	de, err = fs.ReadDir(fsys, "instance/network-interfaces")
	require.NoError(t, err)
	assert.Len(t, de, 1)

	f, err := fsys.Open("instance/service-accounts")
	require.NoError(t, err)
	require.Implements(t, (*fs.ReadDirFile)(nil), f)

	dir := f.(fs.ReadDirFile)

	de, err = dir.ReadDir(-1)
	require.NoError(t, err)
	assert.Len(t, de, 1)
}
