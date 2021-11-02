# hairyhenderson/go-fsimpl

[![GoDoc][godoc-image]][godocs]
[![Build][gh-actions-image]][gh-actions-url]

This module contains a collection of Go _filesystem implementations_ that can
be discovered dynamically by URL scheme. All filesystems are read-only.

These filesystems implement the [`fs.FS`](https://pkg.go.dev/io/fs#FS) interface
[introduced in Go 1.16]()

Most implementations implement the [`fs.ReadDirFS`](https://pkg.go.dev/io/fs#ReadDirFS)
interface, though the `https` filesystem does not.

Some extensions are available to help add specific functionality to certain
filesystems:
- `WithContextFS` - injects a context into a filesystem, for propagating
	cancellation in filesystems that support it.
- `WithHeaderFS` - sets the `http.Header` for all HTTP requests used by the
	filesystem. This can be useful for authentication, or for requesting
	specific content types.
- `WithHTTPClientFS` - sets the `*http.Client` for all HTTP requests to be made
	with.

This module also provides `ContentType`, an extension to the
[`fs.FileInfo`](https://pkg.go.dev/io/fs#FileInfo) type to help identify an
appropriate MIME content type for a given file. For filesystems that support it,
the HTTP `Content-Type` header is used for this. Otherwise, the type is guessed
from the file extension.

## History & Project Status

This module is _in development_, and the API is still subject to change. The
filesystems that are supported should operate correctly.

Most of these filesystems are based on code from [gomplate](https://github.com/hairyhenderson/gomplate),
which supports all of these as datasources. This module is intended to 
eventually be used within gomplate.

## Supported Filesystems

Here's the list of planned filesystem support, along with status:

| Scheme(s) | Description | Supported? |
|-----------|-------------|:----------:|
| [`aws+sm`](./url_schemes.md#awssm) | [AWS Secrets Manager][] | ✅ |
| [`aws+smp`](./url_schemes.md#awssmp) | [AWS Systems Manager Parameter Store][AWS SMP] | |
| [`azblob`](./url_schemes.md#azblob) | [Azure Blob Storage][] | ✅ |
| [`consul`, `consul+http`, `consul+https`](./url_schemes.md#consul) | [HashiCorp Consul][] | |
| [`file`](./url_schemes.md#file) | local filesystem | ✅ |
| [`git`, `git+file`, `git+http`, `git+https`, `git+ssh`](./url_schemes.md#git) | local/remote git repository | ✅ |
| [`gs`](./url_schemes.md#gs) | [Google Cloud Storage][] | ✅ |
| [`http`, `https`](./url_schemes.md#http) | HTTP server | ✅ |
| [`s3`](./url_schemes.md#s3) | [Amazon S3][] | ✅ |
| [`vault`, `vault+http`, `vault+https`](./url_schemes.md#vault) | [HashiCorp Vault][] | ✅ |

See [`url_schemes.md`](./url_schemes.md) for more details on each scheme.

## Installation

You need Go 1.16 or above to use this module. Use `go get` to install the latest
version of `go-fsimpl`:

```console
$ go get -u github.com/hairyhenderson/go-fsimpl
```

## Usage

If you know that you want an HTTP filesystem, for example:

```go
import (
	"net/url"

	"github.com/hairyhenderson/go-fsimpl/httpfs"
)

func main() {
	base, _ := url.Parse("https://example.com")
	fsys, _ := httpfs.New(base)

	f, _ := fsys.Open("hello.txt")
	defer f.Close()

	// now do what you like with the file...
}
```

If you're not sure what filesystem type you need (for example, if you're dealing
with a user-provided URL), you can use a filesystem mux:

```go
import (
	"github.com/hairyhenderson/go-fsimpl"
	"github.com/hairyhenderson/go-fsimpl/blobfs"
	"github.com/hairyhenderson/go-fsimpl/filefs"
	"github.com/hairyhenderson/go-fsimpl/gitfs"
	"github.com/hairyhenderson/go-fsimpl/httpfs"
)

func main() {
	mux := fsimpl.NewMux()
	mux.Add(filefs.FS)
	mux.Add(httpfs.FS)
	mux.Add(blobfs.FS)
	mux.Add(gitfs.FS)

	// for example, a URL that points to a subdirectory at a specific tag in a
	// given git repo, hosted on GitHub and authenticated with SSH...
	fsys, err := mux.Lookup("git+ssh://git@github.com/foo/bar.git//baz#refs/tags/v1.0.0")
	if err != nil {
		log.Fatal(err)
	}

	f, _ := fsys.Open("hello.txt")
	defer f.Close()

	// now do what you like with the file...
}
```

## License

[The MIT License](http://opensource.org/licenses/MIT)

Copyright (c) 2021 Dave Henderson

[godocs]: https://pkg.go.dev/github.com/hairyhenderson/go-fsimpl
[godoc-image]: https://pkg.go.dev/badge/github.com/hairyhenderson/go-fsimpl
[gh-actions-image]: https://github.com/hairyhenderson/go-fsimpl/workflows/Build/badge.svg?branch=main
[gh-actions-url]: https://github.com/hairyhenderson/go-fsimpl/actions?workflow=Build&branch=main

[AWS SMP]: https://aws.amazon.com/systems-manager/features#Parameter_Store
[AWS Secrets Manager]: https://aws.amazon.com/secrets-manager
[HashiCorp Consul]: https://consul.io
[HashiCorp Vault]: https://vaultproject.io
[Amazon S3]: https://aws.amazon.com/s3/
[Google Cloud Storage]: https://cloud.google.com/storage/
[Azure Blob Storage]: https://azure.microsoft.com/en-us/services/storage/blobs/
