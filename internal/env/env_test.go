package env

import (
	"io/fs"
	"os"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
)

func TestGetenv(t *testing.T) {
	assert.Empty(t, Getenv("FOOBARBAZ"))
	assert.Equal(t, os.Getenv("USER"), Getenv("USER"))
	assert.Equal(t, "default value", Getenv("BLAHBLAHBLAH", "default value"))
}

func TestGetenvFile(t *testing.T) {
	fsys := fstest.MapFS{
		"tmp":     &fstest.MapFile{Mode: fs.ModeDir},
		"tmp/foo": &fstest.MapFile{Data: []byte("foo")},
	}

	defer os.Unsetenv("FOO_FILE")
	os.Setenv("FOO_FILE", "/tmp/foo")
	assert.Equal(t, "foo", getenvVFS(fsys, "FOO", "bar"))

	os.Setenv("FOO_FILE", "/tmp/missing")
	assert.Equal(t, "bar", getenvVFS(fsys, "FOO", "bar"))

	fsys["tmp/unreadable"] = &fstest.MapFile{Mode: 0o100}

	os.Setenv("FOO_FILE", "/tmp/unreadable")
	assert.Equal(t, "bar", getenvVFS(fsys, "FOO", "bar"))
}
