package main

import (
	"bytes"
	"io/fs"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLs(t *testing.T) {
	fsys := fstest.MapFS{}

	w := &bytes.Buffer{}

	err := fsLs(fsys, ".", w)
	require.NoError(t, err)
	assert.Empty(t, w.String())

	mtime := time.Unix(0, 0).UTC()

	fsys = fstest.MapFS{
		"a": {ModTime: mtime, Mode: 0o444, Data: []byte("a")},
		"b": {
			ModTime: mtime.AddDate(1, 1, 1).Add(12 * time.Hour),
			Mode:    0o550, Data: bytes.Repeat([]byte("b"), 512),
		},
		"c":        {ModTime: mtime, Mode: 0o400, Data: bytes.Repeat([]byte("c"), 2560)},
		"emptydir": {ModTime: mtime, Mode: 0o555 | fs.ModeDir},
		"dir":      {ModTime: mtime, Mode: 0o555 | fs.ModeDir},
		"dir/a":    {ModTime: mtime, Mode: 0o444, Data: []byte("aa")},
		"dir/b":    {ModTime: mtime, Mode: 0o444, Data: []byte("bb")},
	}

	err = fsLs(fsys, ".", w)
	require.NoError(t, err)
	assert.Equal(t, ` -r--r--r--     1B 1970-01-01 00:00 a
 -r-xr-x---   512B 1971-02-02 12:00 b
 -r-------- 2.5KiB 1970-01-01 00:00 c
 dr-xr-xr-x        1970-01-01 00:00 dir
 dr-xr-xr-x        1970-01-01 00:00 emptydir
`, w.String())
}

func TestCat(t *testing.T) {
	fsys := fstest.MapFS{
		"a": {ModTime: time.Unix(0, 0).UTC(), Mode: 0o444, Data: []byte("aaa")},
	}

	w := &bytes.Buffer{}

	err := cat(fsys, []string{"a"}, w)
	require.NoError(t, err)
	assert.Equal(t, "aaa", w.String())
}

func TestStat(t *testing.T) {
	fsys := fstest.MapFS{
		"a.txt": {ModTime: time.Unix(0, 0).UTC(), Mode: 0o444, Data: []byte("aaa")},
	}

	w := &bytes.Buffer{}

	err := fsStat(fsys, "a.txt", w)
	require.NoError(t, err)
	assert.Equal(t, `a.txt:
	Size:         3B
	Modified:     1970-01-01T00:00:00Z
	Mode:         -r--r--r--
	Content-Type: text/plain; charset=utf-8
`, w.String())
}
