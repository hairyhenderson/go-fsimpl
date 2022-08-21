package main

import (
	"bytes"
	"io/fs"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestLs(t *testing.T) {
	fsys := fstest.MapFS{}

	w := &bytes.Buffer{}

	err := ls(fsys, ".", w)
	assert.NoError(t, err)
	assert.Equal(t, "", w.String())

	mtime := time.Unix(0, 0).UTC()

	fsys = fstest.MapFS{
		"a": {ModTime: mtime, Mode: 0o644, Data: []byte("a")},
		"b": {
			ModTime: mtime.AddDate(1, 1, 1).Add(12 * time.Hour),
			Mode:    0o750, Data: bytes.Repeat([]byte("b"), 512),
		},
		"c":        {ModTime: mtime, Mode: 0o600, Data: bytes.Repeat([]byte("c"), 2560)},
		"emptydir": {ModTime: mtime, Mode: 0o755 | fs.ModeDir},
		"dir":      {ModTime: mtime, Mode: 0o755 | fs.ModeDir},
		"dir/a":    {ModTime: mtime, Mode: 0o644, Data: []byte("aa")},
		"dir/b":    {ModTime: mtime, Mode: 0o644, Data: []byte("bb")},
	}

	err = ls(fsys, ".", w)
	assert.NoError(t, err)
	assert.Equal(t, ` -rw-r--r--     1B 1970-01-01 00:00 a
 -rwxr-x---   512B 1971-02-02 12:00 b
 -rw------- 2.5KiB 1970-01-01 00:00 c
 drwxr-xr-x        1970-01-01 00:00 dir
 drwxr-xr-x        1970-01-01 00:00 emptydir
`, w.String())
}

func TestCat(t *testing.T) {
	fsys := fstest.MapFS{
		"a": {ModTime: time.Unix(0, 0).UTC(), Mode: 0o644, Data: []byte("aaa")},
	}

	w := &bytes.Buffer{}

	err := cat(fsys, []string{"a"}, w)
	assert.NoError(t, err)
	assert.Equal(t, "aaa", w.String())
}

func TestStat(t *testing.T) {
	fsys := fstest.MapFS{
		"a.txt": {ModTime: time.Unix(0, 0).UTC(), Mode: 0o644, Data: []byte("aaa")},
	}

	w := &bytes.Buffer{}

	err := stat(fsys, "a.txt", w)
	assert.NoError(t, err)
	assert.Equal(t, `a.txt:
	Size:         3B
	Modified:     1970-01-01T00:00:00Z
	Mode:         -rw-r--r--
	Content-Type: text/plain; charset=utf-8
`, w.String())
}
