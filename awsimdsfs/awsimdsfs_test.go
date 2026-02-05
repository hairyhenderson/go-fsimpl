package awsimdsfs

import (
	"io"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/hairyhenderson/go-fsimpl/internal/tests"
	"github.com/hairyhenderson/go-fsimpl/internal/tests/fakeimds"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupAWSIMDSFsys(t *testing.T, dir string) fs.FS {
	t.Helper()

	_, u := fakeimds.Server(t)
	u.Path = dir

	fsys, err := New(u)
	require.NoError(t, err)

	return fsys
}

func TestAWSIMDSFS_TestFS(t *testing.T) {
	fsys := setupAWSIMDSFsys(t, "")

	require.NoError(t, fstest.TestFS(fsys, "meta-data", "dynamic", "user-data"))
}

func TestAWSIMDSFS_New(t *testing.T) {
	_, err := New(tests.MustURL("https://example.com"))
	require.Error(t, err)
}

func TestAWSIMDSFS_Stat(t *testing.T) {
	fsys := setupAWSIMDSFsys(t, "")

	f, err := fsys.Open("meta-data")
	require.NoError(t, err)

	fi, err := f.Stat()
	require.NoError(t, err)
	assert.True(t, fi.IsDir())
	assert.Equal(t, "meta-data", fi.Name())

	f, err = fsys.Open("meta-data/foo")
	require.NoError(t, err)

	_, err = f.Stat()
	require.Error(t, err)
	require.ErrorIs(t, err, fs.ErrNotExist)

	_, err = fs.Stat(fsys, "noleadingslash")
	require.Error(t, err)
	require.ErrorIs(t, err, fs.ErrNotExist)

	fi, err = fs.Stat(fsys, "meta-data")
	require.NoError(t, err)
	assert.True(t, fi.IsDir())
}

func TestAWSIMDSFS_Read(t *testing.T) {
	fsys := setupAWSIMDSFsys(t, "")

	f, err := fsys.Open("meta-data/ami-id")
	require.NoError(t, err)

	b, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, "ami-0a887e401f7654935", string(b))

	fsys, err = fs.Sub(fsys, "meta-data/services")
	require.NoError(t, err)

	f, err = fsys.Open("domain")
	require.NoError(t, err)

	defer f.Close()

	b, err = io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, "amazonaws.com", string(b))

	_, err = f.Read(b)
	require.Error(t, err)
}

func TestAWSIMDSFS_ReadFile(t *testing.T) {
	fsys := setupAWSIMDSFsys(t, "")

	b, err := fs.ReadFile(fsys, "meta-data/instance-id")
	require.NoError(t, err)
	assert.Equal(t, "i-1234567890abcdef0", string(b))

	// should error because this is a directory
	_, err = fs.ReadFile(fsys, "dynamic")
	require.ErrorIs(t, err, errIsDirectory)

	subfsys, err := fs.Sub(fsys, "user-data")
	require.NoError(t, err)

	b, err = fs.ReadFile(subfsys, ".")

	require.NoError(t, err)
	assert.Equal(t, "1234,john,reboot,true\n", string(b))
}

func TestAWSIMDSFS_ReadDir(t *testing.T) {
	fsys := setupAWSIMDSFsys(t, "")

	de, err := fs.ReadDir(fsys, ".")
	require.NoError(t, err)
	assert.Len(t, de, 3)

	for i, ex := range []string{"dynamic", "meta-data"} {
		assert.Equal(t, ex, de[i].Name())
		assert.True(t, de[i].IsDir())
	}

	assert.Equal(t, "user-data", de[2].Name())
	assert.False(t, de[2].IsDir())

	fsys = setupAWSIMDSFsys(t, "meta-data")

	de, err = fs.ReadDir(fsys, ".")
	require.NoError(t, err)
	assert.Len(t, de, 28)

	testdata := []struct {
		name string
		dir  bool
	}{
		{"ami-id", false},
		{"ami-launch-index", false},
		{"ami-manifest-path", false},
		{"block-device-mapping", true},
		{"elastic-inference", false},
		{"events", true},
		{"hostname", false},
		{"iam", true},
		{"instance-action", false},
		{"instance-id", false},
	}
	for i, d := range testdata {
		var fi fs.FileInfo
		fi, err = de[i].Info()
		require.NoError(t, err)
		assert.Equal(t, d.name, fi.Name())
		assert.Equal(t, d.dir, fi.IsDir())
	}

	de, err = fs.ReadDir(fsys, "block-device-mapping")
	require.NoError(t, err)
	assert.Len(t, de, 5)

	f, err := fsys.Open("iam")
	require.NoError(t, err)
	require.Implements(t, (*fs.ReadDirFile)(nil), f)

	dir := f.(fs.ReadDirFile)

	de, err = dir.ReadDir(-1)
	require.NoError(t, err)
	assert.Len(t, de, 2)
}
