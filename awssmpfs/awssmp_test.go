package awssmpfs

import (
	"io"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/hairyhenderson/go-fsimpl/internal/tests"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupAWSSMPFsys(t *testing.T, dir string) fs.FS {
	fsys, err := New(tests.MustURL("aws+smp:///" + dir))
	require.NoError(t, err)

	fsys = WithClientFS(clientWithValues(t, map[string]*testVal{
		"/noleadingslash": vs("hiyo"),
		"/notsub/bogus":   vs("not part of /sub"),
		"/sub/a/aa":       vss("secure string aa"),
		"/sub/a/ab":       vs("aab"),
		"/sub/a/ac":       vl("one,two,three"),
		"/sub/b/ba/baa":   vs("bbabaa"),
		"/sub/b/ba/bab":   vs("bbabab"),
		"/sub/b/bb/bba":   vs("bbbbba"),
		"/sub/b/bb/bbb":   vs("bbbbbb"),
		"/sub/b/bb/bbc":   vs("bbbbbc"),
		"/sub/b/bc/bca":   vs(string([]byte{0xde, 0xad, 0xbe, 0xef})),
		"/sub/blah":       vs("blah"),
		"/sub/c/ca/caa":   vs("ccacaa"),
		"/sub/c/cb":       vs("ccb"),
	}), fsys)

	return fsys
}

func TestAWSSMPFS_TestFS(t *testing.T) {
	fsys := setupAWSSMPFsys(t, "")

	require.NoError(t, fstest.TestFS(fsys,
		"noleadingslash",
		"notsub/bogus",
		"sub/a", "sub/b", "sub/c",
		"sub/b/ba", "sub/b/bb", "sub/b/bc", "sub/c/ca",
		"sub/a/aa", "sub/a/ab", "sub/a/ac",
		"sub/b/ba/baa", "sub/b/ba/bab",
		"sub/b/bb/bba", "sub/b/bb/bbb", "sub/b/bb/bbc",
		"sub/b/bc/bca",
		"sub/c/ca/caa",
		"sub/c/cb",
	))

	// test subdirectory
	fsys = setupAWSSMPFsys(t, "sub")

	require.NoError(t, fstest.TestFS(fsys,
		"a", "b", "c",
		"b/ba", "b/bb", "b/bc", "c/ca",
		"a/aa", "a/ab", "a/ac",
		"b/ba/baa", "b/ba/bab",
		"b/bb/bba", "b/bb/bbb", "b/bb/bbc",
		"b/bc/bca",
		"c/ca/caa",
		"c/cb",
	))
}

func TestAWSSMPFS_New(t *testing.T) {
	_, err := New(tests.MustURL("https://example.com"))
	require.Error(t, err)
}

func TestAWSSMPFS_Stat(t *testing.T) {
	fsys := setupAWSSMPFsys(t, "sub")

	f, err := fsys.Open(".")
	require.NoError(t, err)

	fi, err := f.Stat()
	require.NoError(t, err)
	assert.True(t, fi.IsDir())
	assert.Empty(t, fi.Name())

	f, err = fsys.Open("missing")
	require.NoError(t, err)

	_, err = f.Stat()
	require.ErrorIs(t, err, fs.ErrNotExist)

	_, err = fs.Stat(fsys, "noleadingslash")
	require.ErrorIs(t, err, fs.ErrNotExist)

	fi, err = fs.Stat(fsys, "a")
	require.NoError(t, err)
	assert.True(t, fi.IsDir())

	// stat shouldn't leak AWS error types when GetParametersByPath errors
	fsys, err = New(tests.MustURL("aws+smp:///"))
	require.NoError(t, err)

	// shouldn't leak AWS error types when GetParameter errors
	fsys = WithClientFS(clientWithValues(t,
		map[string]*testVal{},
		&types.InvalidParameters{},
	), fsys)

	_, err = fs.Stat(fsys, "blah")
	require.ErrorIs(t, err, fs.ErrInvalid)
}

func TestAWSSMPFS_Read(t *testing.T) {
	fsys := setupAWSSMPFsys(t, "")

	// /sub/a/aa is a SecureString, so make sure it's decrypted
	f, err := fsys.Open("sub/a/aa")
	require.NoError(t, err)

	b, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, "secure string aa", string(b))

	fsys, err = fs.Sub(fsys, "sub/b/bb")
	require.NoError(t, err)

	f, err = fsys.Open("bbc")
	require.NoError(t, err)

	defer f.Close()

	b, err = io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, "bbbbbc", string(b))

	_, err = f.Read(b)
	require.Error(t, err)

	// read shouldn't leak AWS error types when GetParameter errors
	fsys, err = New(tests.MustURL("aws+smp:///"))
	require.NoError(t, err)

	fsys = WithClientFS(clientWithValues(t,
		map[string]*testVal{},
		&types.InternalServerError{Message: aws.String("foo")},
	), fsys)

	_, err = fs.ReadFile(fsys, "blah")
	assert.EqualError(t, err, "readFile blah: internal error: InternalServerError: foo")
}

func TestAWSSMPFS_ReadFile(t *testing.T) {
	fsys := setupAWSSMPFsys(t, "")

	b, err := fs.ReadFile(fsys, "sub/a/aa")
	require.NoError(t, err)
	assert.Equal(t, "secure string aa", string(b))

	fsys, err = fs.Sub(fsys, "sub/a")
	require.NoError(t, err)

	b, err = fs.ReadFile(fsys, "ab")
	require.NoError(t, err)
	assert.Equal(t, "aab", string(b))

	// /sub/a/ac is a StringList, so make sure it's not mangled
	b, err = fs.ReadFile(fsys, "ac")
	require.NoError(t, err)
	assert.Equal(t, "one,two,three", string(b))
}

func TestAWSSMPFS_ReadDir(t *testing.T) {
	fsys, err := New(tests.MustURL("aws+smp:///"))
	require.NoError(t, err)

	fsys = WithClientFS(clientWithValues(t, nil), fsys)

	_, err = fs.ReadDir(fsys, "dir1")
	require.Error(t, err)
	require.ErrorIs(t, err, fs.ErrNotExist)

	fsys = setupAWSSMPFsys(t, "")

	de, err := fs.ReadDir(fsys, ".")
	require.NoError(t, err)
	assert.Len(t, de, 3)

	assert.Equal(t, "noleadingslash", de[0].Name())
	assert.Equal(t, "notsub", de[1].Name())
	assert.Equal(t, "sub", de[2].Name())
	assert.False(t, de[0].IsDir())
	assert.True(t, de[1].IsDir())
	assert.True(t, de[2].IsDir())

	fsys = setupAWSSMPFsys(t, "sub")

	de, err = fs.ReadDir(fsys, ".")
	require.NoError(t, err)
	assert.Len(t, de, 4)

	testdata := []struct {
		name string
		dir  bool
	}{
		{"a", true}, {"b", true}, {"blah", false}, {"c", true},
	}
	for i, d := range testdata {
		var fi fs.FileInfo
		fi, err = de[i].Info()
		require.NoError(t, err)
		assert.Equal(t, d.name, fi.Name())
		assert.Equal(t, d.dir, fi.IsDir())
	}

	de, err = fs.ReadDir(fsys, "b")
	require.NoError(t, err)
	assert.Len(t, de, 3)

	f, err := fsys.Open("b/bb")
	require.NoError(t, err)
	require.Implements(t, (*fs.ReadDirFile)(nil), f)

	dir := f.(fs.ReadDirFile)

	de, err = dir.ReadDir(-1)
	require.NoError(t, err)
	assert.Len(t, de, 3)
}
