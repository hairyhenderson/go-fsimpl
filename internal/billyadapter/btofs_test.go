//go:build !windows

package billyadapter

import (
	"testing"
	"testing/fstest"
	"time"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/stretchr/testify/assert"
)

func createMemFS(t *testing.T) billy.Filesystem {
	bfs := memfs.New()
	_ = bfs.MkdirAll("/dir/subdir", 0o755)

	f, err := bfs.Create("/foo")
	assert.NoError(t, err)

	_, err = f.Write([]byte("hello world"))
	assert.NoError(t, err)

	f, err = bfs.Create("/dir/subdir/bar")
	assert.NoError(t, err)

	_, err = f.Write([]byte("hello"))
	assert.NoError(t, err)

	return FrozenModTimeFilesystem(bfs, time.Now())
}

func TestBillyFS(t *testing.T) {
	bfs := createMemFS(t)

	fsys := BillyToFS(bfs)

	_, err := fsys.Open(".")
	assert.NoError(t, err)

	assert.NoError(t, fstest.TestFS(fsys, "foo", "dir/subdir/bar"))
}
