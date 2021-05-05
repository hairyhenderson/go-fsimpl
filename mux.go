package fsimpl

import (
	"fmt"
	"io/fs"
	"net/url"
)

// FSMux allows you to dynamically look up a registered filesystem for a given
// URL. All filesystems provided in this module can be registered, and
// additional filesystems can be registered given an implementation of
// FSProvider.
type FSMux map[string]func(*url.URL) (fs.FS, error)

// NewMux returns an FSMux ready for use.
func NewMux() FSMux {
	return FSMux(map[string]func(*url.URL) (fs.FS, error){})
}

// Add registers the given filesystem provider for its supported URL schemes. If
// any of its schemes are already registered, they will be overridden.
func (m FSMux) Add(fs FSProvider) {
	for _, scheme := range fs.Schemes() {
		m[scheme] = fs.New
	}
}

// Lookup returns an appropriate filesystem for the given URL. Use Add to
// register providers.
func (m FSMux) Lookup(u string) (fs.FS, error) {
	base, err := url.Parse(u)
	if err != nil {
		return nil, err
	}

	f, ok := m[base.Scheme]
	if !ok {
		return nil, fmt.Errorf("no filesystem registered for scheme %q", base.Scheme)
	}

	return f(base)
}

// FSProvider is able to create filesystems for a set of schemes
type FSProvider interface {
	// Schemes returns the valid URL schemes for this filesystem
	Schemes() []string

	// New returns a filesystem from the given URL
	New(u *url.URL) (fs.FS, error)
}

// FSProviderFunc -
func FSProviderFunc(f func(*url.URL) (fs.FS, error), schemes ...string) FSProvider {
	return fsp{f, schemes}
}

type fsp struct {
	newFunc func(*url.URL) (fs.FS, error)
	schemes []string
}

func (p fsp) Schemes() []string {
	return p.schemes
}

func (p fsp) New(u *url.URL) (fs.FS, error) {
	return p.newFunc(u)
}
