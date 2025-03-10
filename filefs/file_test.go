package filefs

import (
	"net/url"
	"testing"
	"testing/fstest"
	"time"

	"github.com/hairyhenderson/go-fsimpl/internal/tests"
	"github.com/stretchr/testify/assert"
	tfs "gotest.tools/v3/fs"
)

func setupFileSystem(t *testing.T) *tfs.Dir {
	staticTimeStamps := tfs.WithTimestamps(
		time.Date(2022, 8, 1, 13, 14, 15, 0, time.UTC),
		time.Date(2022, 8, 1, 13, 14, 15, 0, time.UTC),
	)

	tmpDir := tfs.NewDir(t, "go-fsimplTests",
		tfs.WithFile("hello.txt", "hello world\n", staticTimeStamps),
		tfs.WithDir("sub",
			tfs.WithFile("subfile.txt", "hi there", staticTimeStamps),
			staticTimeStamps,
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

func TestPathForDirFS(t *testing.T) {
	testdata := []struct {
		in  *url.URL
		out string
	}{
		{tests.MustURL("file:"), ""},
		{tests.MustURL("file:/"), "/"},
		{tests.MustURL("file:///"), "/"},
		{tests.MustURL("file:///tmp/foo"), "/tmp/foo"},
		{tests.MustURL("file:///C:/Users/"), "C:/Users/"},
		{tests.MustURL("file:///C:/Program%20Files"), "C:/Program Files"},
		{tests.MustURL("file://./C:/Users/"), "//./C:/Users/"},
		{tests.MustURL("file://somehost/Share/foo"), "//somehost/Share/foo"},
		{tests.MustURL("file://invalid"), ""},
		{tests.MustURL("file://host/j"), "//host/j"},
	}

	for _, d := range testdata {
		assert.Equal(t, d.out, pathForDirFS(d.in))
	}
}

func BenchmarkPathForDirFS(b *testing.B) {
	testdata := []*url.URL{
		tests.MustURL("file:"),
		tests.MustURL("file:/"),
		tests.MustURL("file:///"),
		tests.MustURL("file:///tmp/foo"),
		tests.MustURL("file:///C:/Users/"),
		tests.MustURL("file:///C:/Program%20Files"),
		tests.MustURL("file://./C:/Users/"),
		tests.MustURL("file://somehost/Share/foo"),
		tests.MustURL("file://invalid"),
	}

	// reset the timer after setup - the above adds a few allocations
	b.ResetTimer()

	for b.Loop() {
		for _, d := range testdata {
			pathForDirFS(d)
		}
	}
}
