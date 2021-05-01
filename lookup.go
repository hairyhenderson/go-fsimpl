package fsimpl

import (
	"fmt"
	"io/fs"
	"net/url"
)

// constants for supported URL schemes
const (
	schemeFile     = "file"
	schemeHTTP     = "http"
	schemeHTTPS    = "https"
	schemeGit      = "git"
	schemeGitFile  = "git+file"
	schemeGitHTTP  = "git+http"
	schemeGitHTTPS = "git+https"
	schemeGitSSH   = "git+ssh"
	schemeS3       = "s3"
	schemeSSH      = "ssh"
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
	case schemeFile:
		return FileFS(base.Path), nil
	case schemeHTTP, schemeHTTPS:
		return HTTPFS(base), nil
	case "vault", "vault+http", "vault+https":
	case schemeS3:
		return S3FS(base)
	case "gs":
	case schemeGit, schemeGitFile, schemeGitHTTP, schemeGitHTTPS, schemeGitSSH:
		return GitFS(base), nil
	default:
		return nil, fmt.Errorf("no filesystem available for scheme %q", base.Scheme)
	}

	return nil, fmt.Errorf("not implemented")
}
