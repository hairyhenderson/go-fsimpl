package fsimpl

import (
	"testing"
	"time"

	"github.com/hairyhenderson/go-fsimpl/internal"
	"github.com/stretchr/testify/assert"
)

func TestContentType(t *testing.T) {
	fi := internal.FileInfo("foo", 0, 0, time.Time{}, "")
	assert.Equal(t, "", ContentType(fi))

	fi = internal.FileInfo("foo", 0, 0, time.Time{}, "text/plain")
	assert.Equal(t, "text/plain", ContentType(fi))

	fi = internal.FileInfo("foo.json", 0, 0, time.Time{}, "text/plain")
	assert.Equal(t, "text/plain", ContentType(fi))

	fi = internal.FileInfo("foo.json", 0, 0, time.Time{}, "")
	assert.Equal(t, "application/json", ContentType(fi))
}
