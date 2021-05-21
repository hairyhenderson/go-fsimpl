package filefs

import (
	"net/url"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"gotest.tools/v3/fs"
)

func setupFileSystem(t *testing.T) *fs.Dir {
	tmpDir := fs.NewDir(t, "go-fsimplTests",
		fs.WithFile("hello.txt", "hello world\n"),
		fs.WithDir("sub",
			fs.WithFile("subfile.txt", "hi there"),
		),
	)
	t.Cleanup(tmpDir.Remove)

	return tmpDir
}

func TestFileFS(t *testing.T) {
	tmpDir := setupFileSystem(t)

	fsys, _ := New(&url.URL{Path: tmpDir.Path()})

	err := fstest.TestFS(fsys, "hello.txt", "sub/subfile.txt")
	assert.NoError(t, err)
}

func mustURL(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(err)
	}

	return u
}

func TestPathForDirFS(t *testing.T) {
	testdata := []struct {
		in  *url.URL
		out string
	}{
		{mustURL("file:"), ""},
		{mustURL("file:/"), "/"},
		{mustURL("file:///"), "/"},
		{mustURL("file:///tmp/foo"), "/tmp/foo"},
		{mustURL("file:///C:/Users/"), "C:/Users/"},
		{mustURL("file:///C:/Program%20Files"), "C:/Program Files"},
		{mustURL("file://./C:/Users/"), "//./C:/Users/"},
		{mustURL("file://somehost/Share/foo"), "//somehost/Share/foo"},
		{mustURL("file://invalid"), ""},
		{mustURL("file://host/j"), "//host/j"},
	}

	for _, d := range testdata {
		assert.Equal(t, d.out, pathForDirFS(d.in))
	}
}

func BenchmarkPathForDirFS(b *testing.B) {
	testdata := []*url.URL{
		mustURL("file:"),
		mustURL("file:/"),
		mustURL("file:///"),
		mustURL("file:///tmp/foo"),
		mustURL("file:///C:/Users/"),
		mustURL("file:///C:/Program%20Files"),
		mustURL("file://./C:/Users/"),
		mustURL("file://somehost/Share/foo"),
		mustURL("file://invalid"),
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		for _, d := range testdata {
			pathForDirFS(d)
		}
	}
}
