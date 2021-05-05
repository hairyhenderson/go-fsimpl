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
