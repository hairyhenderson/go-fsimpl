// Package autofs provides the ability to look up all filesystems supported by
// this module. Using this package will compile a great many dependencies into
// the resulting binary, so unless you need to support all supported filesystems,
// use fsimpl.NewMux instead.
package autofs

import (
	"io/fs"
	"sync"

	"github.com/hairyhenderson/go-fsimpl"
	"github.com/hairyhenderson/go-fsimpl/awssmfs"
	"github.com/hairyhenderson/go-fsimpl/blobfs"
	"github.com/hairyhenderson/go-fsimpl/filefs"
	"github.com/hairyhenderson/go-fsimpl/gitfs"
	"github.com/hairyhenderson/go-fsimpl/httpfs"
	"github.com/hairyhenderson/go-fsimpl/vaultfs"
)

//nolint:gochecknoglobals
var (
	mux     fsimpl.FSMux
	muxInit sync.Once
)

// Lookup returns an appropriate filesystem for the given URL.
// If a filesystem can't be found for the provided URL's scheme, an error will
// be returned.
func Lookup(u string) (fs.FS, error) {
	muxInit.Do(func() {
		mux = fsimpl.NewMux()
		mux.Add(filefs.FS)
		mux.Add(httpfs.FS)
		mux.Add(blobfs.FS)
		mux.Add(gitfs.FS)
		mux.Add(vaultfs.FS)
		mux.Add(awssmfs.FS)
	})

	return mux.Lookup(u)
}
