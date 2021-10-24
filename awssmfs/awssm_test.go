package awssmfs

import (
	"io"
	"io/fs"
	"testing"
	"testing/fstest"

	smtypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/hairyhenderson/go-fsimpl/internal/tests"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupAWSSMFsys(t *testing.T, dir string) fs.FS {
	fsys, err := New(tests.MustURL("aws+sm:///" + dir))
	assert.NoError(t, err)

	fsys = WithSMClientFS(clientWithValues(t, map[string]*testVal{
		"noleadingslash":  vs("shouldn't be read"),
		"noleading/slash": vs("shouldn't be read"),
		"/notsub/bogus":   vs("not part of /sub"),
		"/sub/a/aa":       vs("aaa"),
		"/sub/a/ab":       vs("aab"),
		"/sub/a/ac":       vs("aac"),
		"/sub/b/ba/baa":   vb([]byte("bbabaa")),
		"/sub/b/ba/bab":   vb([]byte("bbabab")),
		"/sub/b/bb/bba":   vb([]byte("bbbbba")),
		"/sub/b/bb/bbb":   vb([]byte("bbbbbb")),
		"/sub/b/bb/bbc":   vb([]byte("bbbbbc")),
		"/sub/b/bc/bca":   vb([]byte{0xde, 0xad, 0xbe, 0xef}),
		"/sub/blah":       vb([]byte("blah")),
		"/sub/c/ca/caa":   vs("ccacaa"),
		"/sub/c/cb":       vs("ccb"),
	}), fsys)

	return fsys
}

func setupOpaqueAWSSMFsys(t *testing.T, prefix string) fs.FS {
	fsys, err := New(tests.MustURL("aws+sm:" + prefix))
	assert.NoError(t, err)

	fsys = WithSMClientFS(clientWithValues(t, map[string]*testVal{
		"/notlisted/bogus": vs("not listed because of the / prefix"),
		"/nope":            vs("not listed because of the / prefix"),
		"blah":             vb([]byte("blah")),
		"a/aa":             vs("aaa"),
		"b/ba/baa":         vb([]byte("bbabaa")),
		"b/ba/bab":         vb([]byte("bbabab")),
		"b/bb/bba":         vb([]byte("bbbbba")),
		"b/bb/bbb":         vb([]byte("bbbbbb")),
		"b/bb/bbc":         vb([]byte("bbbbbc")),
		"b/bc/bca":         vb([]byte{0xde, 0xad, 0xbe, 0xef}),
		"c/cb":             vs("ccb"),
	}), fsys)

	return fsys
}

func TestAWSSMFS_TestFS(t *testing.T) {
	fsys := setupAWSSMFsys(t, "sub")

	assert.NoError(t, fstest.TestFS(fsys,
		"a", "b", "c",
		"b/ba", "b/bb", "b/bc", "c/ca",
		"a/aa", "a/ab", "a/ac",
		"b/ba/baa", "b/ba/bab",
		"b/bb/bba", "b/bb/bbb", "b/bb/bbc",
		"b/bc/bca",
		"c/ca/caa",
		"c/cb",
	))

	// test opaque
	fsys = setupOpaqueAWSSMFsys(t, "b")
	assert.NoError(t, fstest.TestFS(fsys,
		"ba", "bb", "bc",
		"ba/baa", "ba/bab",
		"bb/bba", "bb/bbb", "bb/bbc",
		"bc/bca"))
}

func TestAWSSMFS_New(t *testing.T) {
	_, err := New(tests.MustURL("https://example.com"))
	assert.Error(t, err)
}

func TestAWSSMFS_Stat(t *testing.T) {
	fsys := setupAWSSMFsys(t, "sub")

	f, err := fsys.Open(".")
	assert.NoError(t, err)

	fi, err := f.Stat()
	assert.NoError(t, err)
	assert.True(t, fi.IsDir())
	assert.Equal(t, "", fi.Name())

	f, err = fsys.Open("missing")
	assert.NoError(t, err)

	_, err = f.Stat()
	assert.ErrorIs(t, err, fs.ErrNotExist)

	_, err = fs.Stat(fsys, "noleadingslash")
	assert.ErrorIs(t, err, fs.ErrNotExist)

	fi, err = fs.Stat(fsys, "a")
	assert.NoError(t, err)
	assert.True(t, fi.IsDir())

	fsys = setupOpaqueAWSSMFsys(t, "")

	f, err = fsys.Open(".")
	assert.NoError(t, err)

	fi, err = f.Stat()
	assert.NoError(t, err)
	assert.True(t, fi.IsDir())

	fsys = setupOpaqueAWSSMFsys(t, "b")

	// make sure the file "blah" isn't misinterpreted as "b/lah"
	_, err = fs.Stat(fsys, "lah")
	assert.ErrorIs(t, err, fs.ErrNotExist)

	// stat shouldn't leak AWS error types when ListSecrets errors
	fsys, err = New(tests.MustURL("aws+sm:///"))
	assert.NoError(t, err)

	fsys = WithSMClientFS(clientWithValues(t,
		map[string]*testVal{}, nil,
		&smtypes.DecryptionFailure{},
	), fsys)

	_, err = fs.Stat(fsys, "blah")
	assert.ErrorIs(t, err, fs.ErrPermission)

	// shouldn't leak AWS error types when GetSecretValue errors
	fsys = WithSMClientFS(clientWithValues(t,
		map[string]*testVal{},
		&smtypes.InvalidParameterException{},
	), fsys)

	_, err = fs.Stat(fsys, "blah")
	assert.ErrorIs(t, err, fs.ErrInvalid)
}

func TestAWSSMFS_Read(t *testing.T) {
	fsys := setupAWSSMFsys(t, "")

	f, err := fsys.Open("sub/a/aa")
	assert.NoError(t, err)

	b, err := io.ReadAll(f)
	assert.NoError(t, err)
	assert.Equal(t, "aaa", string(b))

	fsys, err = fs.Sub(fsys, "sub/b/bb")
	assert.NoError(t, err)

	f, err = fsys.Open("bbc")
	assert.NoError(t, err)

	defer f.Close()

	b, err = io.ReadAll(f)
	assert.NoError(t, err)
	assert.Equal(t, "bbbbbc", string(b))

	_, err = f.Read(b)
	assert.Error(t, err)

	// read shouldn't leak AWS error types when GetSecret errors
	fsys, err = New(tests.MustURL("aws+sm:///"))
	assert.NoError(t, err)

	fsys = WithSMClientFS(clientWithValues(t,
		map[string]*testVal{},
		&smtypes.InternalServiceError{Message: aws.String("foo")},
	), fsys)

	_, err = fs.ReadFile(fsys, "blah")
	assert.EqualError(t, err, "readFile blah: internal error: InternalServiceError: foo")
}

func TestAWSSMFS_ReadFile(t *testing.T) {
	fsys := setupAWSSMFsys(t, "")

	b, err := fs.ReadFile(fsys, "sub/a/aa")
	assert.NoError(t, err)
	assert.Equal(t, "aaa", string(b))

	fsys, err = fs.Sub(fsys, "sub/a")
	assert.NoError(t, err)

	b, err = fs.ReadFile(fsys, "ab")
	assert.NoError(t, err)
	assert.Equal(t, "aab", string(b))
}

func TestAWSSMFS_ReadDir(t *testing.T) {
	fsys, err := New(tests.MustURL("aws+sm:///"))
	assert.NoError(t, err)

	fsys = WithSMClientFS(clientWithValues(t, nil), fsys)

	_, err = fs.ReadDir(fsys, "dir1")
	assert.Error(t, err)
	assert.ErrorIs(t, err, fs.ErrNotExist)

	fsys = setupAWSSMFsys(t, "")

	de, err := fs.ReadDir(fsys, ".")
	assert.NoError(t, err)
	assert.Len(t, de, 2)

	for i, ex := range []string{"notsub", "sub"} {
		assert.Equal(t, ex, de[i].Name())
		assert.True(t, de[i].IsDir())
	}

	fsys = setupAWSSMFsys(t, "sub")

	de, err = fs.ReadDir(fsys, ".")
	assert.NoError(t, err)
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
		assert.NoError(t, err)
		assert.Equal(t, d.name, fi.Name())
		assert.Equal(t, d.dir, fi.IsDir())
	}

	de, err = fs.ReadDir(fsys, "b")
	assert.NoError(t, err)
	assert.Len(t, de, 3)

	f, err := fsys.Open("b/bb")
	assert.NoError(t, err)
	require.Implements(t, (*fs.ReadDirFile)(nil), f)

	dir := f.(fs.ReadDirFile)

	de, err = dir.ReadDir(-1)
	assert.NoError(t, err)
	assert.Len(t, de, 3)
}

func TestAWSSMFS_ReadDir_Opaque(t *testing.T) {
	fsys := setupOpaqueAWSSMFsys(t, "")

	de, err := fs.ReadDir(fsys, ".")
	require.NoError(t, err)
	require.Len(t, de, 4)

	fi, err := de[0].Info()
	assert.NoError(t, err)
	assert.Equal(t, "a", fi.Name())
	assert.True(t, fi.IsDir())

	fi, err = de[1].Info()
	assert.NoError(t, err)
	assert.Equal(t, "b", fi.Name())
	assert.True(t, fi.IsDir())

	fi, err = de[2].Info()
	assert.NoError(t, err)
	assert.Equal(t, "blah", fi.Name())
	assert.False(t, fi.IsDir())

	fi, err = de[3].Info()
	assert.NoError(t, err)
	assert.Equal(t, "c", fi.Name())
	assert.True(t, fi.IsDir())
}
