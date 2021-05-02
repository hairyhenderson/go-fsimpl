package fsimpl

import (
	"fmt"
	"io/fs"
	"net/url"

	"gocloud.dev/blob/azureblob"
	"gocloud.dev/blob/gcsblob"
	"gocloud.dev/blob/s3blob"
)

// constants for supported URL schemes
const (
	schemeAzBlob   = azureblob.Scheme
	schemeFile     = "file"
	schemeHTTP     = "http"
	schemeHTTPS    = "https"
	schemeGCS      = gcsblob.Scheme
	schemeGit      = "git"
	schemeGitFile  = "git+file"
	schemeGitHTTP  = "git+http"
	schemeGitHTTPS = "git+https"
	schemeGitSSH   = "git+ssh"
	schemeS3       = s3blob.Scheme
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
	case schemeS3, schemeGCS, schemeAzBlob:
		return BlobFS(base)
	case schemeGit, schemeGitFile, schemeGitHTTP, schemeGitHTTPS, schemeGitSSH:
		return GitFS(base), nil
	default:
		return nil, fmt.Errorf("no filesystem available for scheme %q", base.Scheme)
	}

	return nil, fmt.Errorf("not implemented")
}
