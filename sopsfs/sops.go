package sopsfs

import (
	"context"
	"fmt"
	"github.com/getsops/sops/v3/decrypt"
	"github.com/hairyhenderson/go-fsimpl"
	"github.com/hairyhenderson/go-fsimpl/internal"
	"io"
	"io/fs"
	"net/url"
	"path/filepath"
)

type sopsFS struct {
	ctx  context.Context
	base *url.URL
}

// New provides a filesystem (an fs.FS) for a directory
// rooted at u. This filesystem is suitable for use with the
// 'sops' protocol.
func New(u *url.URL) (fs.FS, error) {
	return &sopsFS{
		base: u,
	}, nil
}

// FS is used to register this filesystem with an fsimpl.FSMux
//
//nolint:gochecknoglobals
var FS = fsimpl.FSProviderFunc(New, "sops")

var (
	_ fs.FS         = (*sopsFS)(nil)
	_ fs.ReadFileFS = (*sopsFS)(nil)
	_ fs.SubFS      = (*sopsFS)(nil)
)

func (f sopsFS) URL() string {
	return f.base.String()
}

func (f sopsFS) Open(name string) (fs.File, error) {
	u, err := internal.SubURL(f.base, name)
	if err != nil {
		return nil, err
	}

	format := u.Query().Get("format")
	if format == "" {
		format = filepath.Ext(name)[1:]
	}

	if format != "json" && format != "yaml" {
		return nil, fmt.Errorf("could not determine sops format; got: %s", format)
	}

	return &sopsFile{
		u:      u,
		format: format,
	}, nil
}

func (f sopsFS) ReadFile(name string) ([]byte, error) {
	opened, err := f.Open(name)
	if err != nil {
		return nil, err
	}
	defer opened.Close()

	b, err := io.ReadAll(opened)
	if err != nil {
		return nil, err
	}

	return b, nil
}

func (f sopsFS) Sub(name string) (fs.FS, error) {
	fsys := f

	u, err := internal.SubURL(f.base, name)
	if err != nil {
		return nil, err
	}

	fsys.base = u

	return &fsys, nil
}

type sopsFile struct {
	fi     fs.FileInfo
	u      *url.URL
	format string
}

func (f *sopsFile) Read(bytes []byte) (int, error) {
	content, err := decrypt.File(f.u.Path, f.format)
	if err != nil {
		return 0, err
	}
	copy(bytes, content)
	return len(content), io.EOF
}

var _ fs.File = (*sopsFile)(nil)

func (f *sopsFile) Close() error {
	return nil
}

func (f *sopsFile) ReadFile([]byte) ([]byte, error) {
	return decrypt.File(f.u.Path, f.format)
}

func (f *sopsFile) Stat() (fs.FileInfo, error) {
	return fs.Stat(nil, f.u.Path)
}
