package fsimpl

import (
	"fmt"
	"io/fs"
	"net/url"
)

// LookupFS returns an appropriate filesystem implementation for the given URL.
// If a filesystem can't be found for the provided URL's scheme, an error will
// be returned.
//
//nolint:gocyclo
func LookupFS(u string) (fs.FS, error) {
	base, err := url.Parse(u)
	if err != nil {
		return nil, err
	}

	switch base.Scheme {
	case "aws+smp":
	case "aws+sm":
	case "boltdb":
	case "consul", "consul+http", "consul+https":
	case "file":
		return FileFS(base.Path), nil
	case "http", "https":
		return HTTPFS(base), nil
	case "vault", "vault+http", "vault+https":
	case "s3", "gs":
	case "git", "git+file", "git+http", "git+https", "git+ssh":
	default:
		return nil, fmt.Errorf("no filesystem available for scheme %q", base.Scheme)
	}

	return nil, fmt.Errorf("not implemented")
}
