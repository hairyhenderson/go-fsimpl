package tracefs

import (
	"time"

	"go.opentelemetry.io/otel/attribute"
)

const (
	typeKey    = attribute.Key("fs.type")
	pathKey    = attribute.Key("fs.path")
	baseURLKey = attribute.Key("fs.base_url")
	patternKey = attribute.Key("fs.pattern")

	direntKey  = attribute.Key("dir.entries")
	sizeKey    = attribute.Key("file.size")
	permsKey   = attribute.Key("file.perms")
	modTimeKey = attribute.Key("file.modtime")

	bytesReadKey = attribute.Key("file.bytes_read")
)

// The type of filesystem being operated on.
//
// Type: string
// Required: No
// Examples: "gitfs", "httpfs", "filefs"
func Type(name string) attribute.KeyValue {
	return typeKey.String(name)
}

// The path being operated on.
//
// Type: string
// Required: Yes
// Examples: "README.md", "example/directory/foo.txt"
func Path(name string) attribute.KeyValue {
	return pathKey.String(name)
}

// The base URL of the file system.
//
// Type: string
// Required: No
// Examples: "https://example.com", "file:///tmp"
func BaseURL(url string) attribute.KeyValue {
	return baseURLKey.String(url)
}

// The pattern used (by Glob) to match files.
//
// Type: string
// Required: No
// Examples: "*.txt", "foo/**"
func Pattern(pattern string) attribute.KeyValue {
	return patternKey.String(pattern)
}

// The number of entries in a directory.
//
// Type: int
// Required: No
// Examples: 3, 0
func DirEntries(n int) attribute.KeyValue {
	return direntKey.Int(n)
}

// The size of a file.
//
// Type: int64
// Required: No
// Examples: 1024, 0
func FileSize(n int64) attribute.KeyValue {
	return sizeKey.Int64(n)
}

// The permissions of a file.
//
// Type: string
// Required: No
// Examples: "-rw-r--r--", "drwxr-xr-x"
func FilePerms(perms string) attribute.KeyValue {
	return permsKey.String(perms)
}

// The modification time of a file.
//
// Type: time.Time
// Required: No
// Examples: "2021-08-21T11:10:00Z", "2021-08-21T11:10:00-07:00"
func FileModTime(t time.Time) attribute.KeyValue {
	return modTimeKey.String(t.Format(time.RFC3339))
}

// The number of bytes read from a file during a Read operation.
//
// Type: int
// Required: No
// Examples: 1024, 0
func FileBytesRead(n int) attribute.KeyValue {
	return bytesReadKey.Int(n)
}
