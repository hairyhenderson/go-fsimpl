//go:build !windows

package billyadapter

import (
	"testing"
	"testing/fstest"
	"time"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/stretchr/testify/require"
)

func createMemFS(t *testing.T) billy.Filesystem {
	bfs := memfs.New()
	_ = bfs.MkdirAll("/dir/subdir", 0o755)

	f, err := bfs.Create("/foo")
	require.NoError(t, err)

	_, err = f.Write([]byte("hello world"))
	require.NoError(t, err)

	f, err = bfs.Create("/dir/subdir/bar")
	require.NoError(t, err)

	_, err = f.Write([]byte("hello"))
	require.NoError(t, err)

	return FrozenModTimeFilesystem(bfs, time.Now())
}

func TestBillyFS(t *testing.T) {
	bfs := createMemFS(t)

	fsys := BillyToFS(bfs)

	_, err := fsys.Open(".")
	require.NoError(t, err)

	require.NoError(t, fstest.TestFS(fsys, "foo", "dir/subdir/bar"))
}
