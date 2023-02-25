package env

import (
	"io/fs"
	"os"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
)

func TestGetenvFS(t *testing.T) {
	fsys := fstest.MapFS{
		"tmp":     &fstest.MapFile{Mode: fs.ModeDir},
		"tmp/foo": &fstest.MapFile{Data: []byte("foo")},
	}

	assert.Empty(t, GetenvFS(fsys, "FOOBARBAZ"))
	assert.Equal(t, os.Getenv("USER"), GetenvFS(fsys, "USER"))
	assert.Equal(t, "default value", GetenvFS(fsys, "BLAHBLAHBLAH", "default value"))

	t.Setenv("FOO_FILE", "/tmp/foo")
	assert.Equal(t, "foo", GetenvFS(fsys, "FOO", "bar"))

	t.Setenv("FOO_FILE", "/tmp/missing")
	assert.Equal(t, "bar", GetenvFS(fsys, "FOO", "bar"))

	fsys["tmp/unreadable"] = &fstest.MapFile{Mode: 0o100}

	t.Setenv("FOO_FILE", "/tmp/unreadable")
	assert.Equal(t, "bar", GetenvFS(fsys, "FOO", "bar"))
}
