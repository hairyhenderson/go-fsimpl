//+build windows

package filefs

import (
	"fmt"
	"io/fs"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	tfs "gotest.tools/v3/fs"
)

func setupWinFileSystem(t *testing.T) *tfs.Dir {
	tmpDir := tfs.NewDir(t, "go-fsimplWinTests",
		tfs.WithFile("hello.txt", "hello world\n"),
		tfs.WithDir("sub",
			tfs.WithFile("subfile.txt", "hi there"),
		),
	)
	t.Cleanup(tmpDir.Remove)

	return tmpDir
}

func TestFileFS_Windows(t *testing.T) {
	tmpDir := setupWinFileSystem(t)
	tmpRoot := filepath.ToSlash(tmpDir.Path())

	u, err := url.Parse("file:///" + tmpRoot)
	assert.NoError(t, err)

	fsys, err := New(u)
	assert.NoError(t, err)

	fileFsys := fsys.(*fileFS)
	assert.Equal(t, tmpRoot, fmt.Sprintf("%v", fileFsys.root))

	des, err := fs.ReadDir(fsys, ".")
	assert.NoError(t, err)
	assert.Len(t, des, 2)

	names := make([]string, 2)
	for i, de := range des {
		names[i] = de.Name()
	}

	assert.Contains(t, names, "sub")
	assert.Contains(t, names, "hello.txt")

	// test a local UNC, and lower-case drive letter etc (case-insensitive, so should match)
	uncURL, err := url.Parse("file://./" + strings.ToLower(tmpRoot))
	assert.NoError(t, err)

	fsys, _ = New(uncURL)

	// case-insensitive, again...
	b, err := fs.ReadFile(fsys, "sUb/SubFile.txt")
	assert.NoError(t, err)
	assert.Equal(t, "hi there", string(b))
}
