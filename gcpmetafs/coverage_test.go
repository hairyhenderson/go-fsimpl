package gcpmetafs

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/compute/metadata"
	"github.com/hairyhenderson/go-fsimpl/internal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockMetadataClient is a simple mock implementation of MetadataClient
type mockMetadataClient struct {
	getFunc func(suffix string) (string, error)
}

func (m *mockMetadataClient) GetWithContext(_ context.Context, suffix string) (string, error) {
	if m.getFunc != nil {
		return m.getFunc(suffix)
	}

	return "value", nil
}

// errorMetadataClient is a mock implementation that always returns an error
type errorMetadataClient struct {
	err string
}

func (m *errorMetadataClient) GetWithContext(_ context.Context, _ string) (string, error) {
	return "", errors.New(m.err)
}

// dummyFS is a simple mock implementation of fs.FS
type dummyFS struct{}

func (d *dummyFS) Open(_ string) (fs.File, error) {
	return nil, nil
}

func TestWithMetadataClientFS(t *testing.T) {
	// Test with a filesystem that doesn't implement withMetadataClienter
	fsys := &dummyFS{}
	newFS := WithMetadataClientFS(&mockMetadataClient{}, fsys)
	assert.Equal(t, fsys, newFS)

	// Test with a filesystem that implements withMetadataClienter
	gcpFS := &gcpmetaFS{
		ctx:        t.Context(),
		metaclient: nil,
	}
	newFS = WithMetadataClientFS(&mockMetadataClient{}, gcpFS)
	assert.NotEqual(t, gcpFS, newFS)
	assert.NotNil(t, newFS.(*gcpmetaFS).metaclient)
}

func TestURL(t *testing.T) {
	u, _ := url.Parse("gcp+meta:///")
	fsys := &gcpmetaFS{
		base: u,
	}
	assert.Equal(t, "gcp+meta:///", fsys.URL())
}

func TestWithHTTPClient(t *testing.T) {
	fsys := &gcpmetaFS{}
	client := &http.Client{}
	newFS := fsys.WithHTTPClient(client)
	assert.Equal(t, client, newFS.(*gcpmetaFS).httpclient)

	// Test with nil client
	newFS = fsys.WithHTTPClient(nil)
	assert.Equal(t, fsys, newFS)
}

func TestWithMetadataClient(t *testing.T) {
	fsys := &gcpmetaFS{}
	client := &mockMetadataClient{}
	newFS := fsys.WithMetadataClient(client)
	assert.Equal(t, client, newFS.(*gcpmetaFS).metaclient)
}

func TestListPrefix(t *testing.T) {
	f := &gcpmetaFile{
		ctx:    t.Context(),
		name:   "test",
		root:   "root",
		client: &mockMetadataClient{},
	}
	assert.Equal(t, "root/test/", f.listPrefix())
}

func TestList(t *testing.T) {
	f := &gcpmetaFile{
		ctx:    t.Context(),
		name:   "test",
		client: &mockMetadataClient{},
	}
	err := f.list()
	require.NoError(t, err)
	assert.NotNil(t, f.children)
}

// mockCloser is a mock implementation of io.ReadCloser
type mockCloser struct {
	closed bool
}

func (m *mockCloser) Close() error {
	m.closed = true

	return nil
}

func (m *mockCloser) Read(_ []byte) (n int, err error) {
	return 0, io.EOF
}

func TestClose(t *testing.T) {
	// Test with nil body
	f := &gcpmetaFile{
		body: nil,
	}
	err := f.Close()
	require.NoError(t, err)

	// Test with body that implements io.Closer
	closer := &mockCloser{}
	f = &gcpmetaFile{
		body: closer,
	}
	err = f.Close()
	require.NoError(t, err)
	assert.True(t, closer.closed)
}

func TestGetValue(t *testing.T) {
	// Test with a directory
	f := &gcpmetaFile{
		ctx:    t.Context(),
		name:   "instance",
		client: &mockMetadataClient{},
	}
	err := f.getValue()
	require.NoError(t, err)
	assert.NotNil(t, f.fi)
	assert.True(t, f.fi.IsDir())

	// Test with a file
	f = &gcpmetaFile{
		ctx:    t.Context(),
		name:   "id",
		client: &mockMetadataClient{},
	}
	err = f.getValue()
	require.NoError(t, err)
	assert.NotNil(t, f.fi)
	assert.False(t, f.fi.IsDir())
}

func TestFetchDirectoryListing(t *testing.T) {
	f := &gcpmetaFile{
		ctx:    t.Context(),
		name:   "test",
		client: &mockMetadataClient{},
	}
	children, err := f.fetchDirectoryListing("test")
	require.NoError(t, err)
	assert.NotNil(t, children)
}

func TestPopulateChildren(t *testing.T) {
	f := &gcpmetaFile{
		ctx:    t.Context(),
		name:   "test",
		client: &mockMetadataClient{},
	}
	err := f.populateChildren([]string{"child1", "child2"})
	require.NoError(t, err)
	assert.Len(t, f.children, 2)
}

func TestReadDirWithError(t *testing.T) {
	// Test ReadDir with an error from list
	f := &gcpmetaFile{
		ctx:    t.Context(),
		name:   "error-dir",
		client: &errorMetadataClient{err: "some error"},
	}
	_, err := f.ReadDir(-1)
	require.Error(t, err)
}

func TestStatWithExistingFileInfo(t *testing.T) {
	// Test Stat with existing file info
	f := &gcpmetaFile{
		fi: internal.FileInfo("test", 0, 0, time.Time{}, ""),
	}
	fi, err := f.Stat()
	require.NoError(t, err)
	assert.Equal(t, "test", fi.Name())
}

func TestSubWithInvalidPath(t *testing.T) {
	fs := &gcpmetaFS{
		ctx:        t.Context(),
		metaclient: &mockMetadataClient{},
	}
	_, err := fs.Sub("../invalid")
	require.Error(t, err)
}

func TestReadWithNilBody(t *testing.T) {
	// Mock client that returns an error for getValue
	client := &mockMetadataClient{
		getFunc: func(_ string) (string, error) {
			return "", errors.New("test error")
		},
	}

	f := &gcpmetaFile{
		ctx:    t.Context(),
		name:   "test",
		client: client,
		// body is nil, which will trigger getValue
	}
	buf := make([]byte, 10)
	_, err := f.Read(buf)
	require.Error(t, err)
}

func TestSubWithValidPath(t *testing.T) {
	fs := &gcpmetaFS{
		ctx:        t.Context(),
		metaclient: &mockMetadataClient{},
		root:       "",
	}
	subFS, err := fs.Sub("instance")
	require.NoError(t, err)
	assert.NotNil(t, subFS)

	// Check that the root was updated
	gcpFS, ok := subFS.(*gcpmetaFS)
	assert.True(t, ok)
	assert.Equal(t, "instance", gcpFS.root)
}

func TestReadDirWithValidPath(t *testing.T) {
	// Mock client that returns a successful directory listing
	client := &mockMetadataClient{
		getFunc: func(suffix string) (string, error) {
			if strings.HasSuffix(suffix, "/") {
				return "id\nname\nzone\n", nil
			}

			return "value", nil
		},
	}

	fs := &gcpmetaFS{
		ctx:        t.Context(),
		metaclient: client,
		root:       "",
	}

	entries, err := fs.ReadDir("instance")
	require.NoError(t, err)
	assert.NotEmpty(t, entries)
}

func TestPopulateChildrenWithEmptyList(t *testing.T) {
	f := &gcpmetaFile{
		ctx:    t.Context(),
		name:   "test",
		client: &mockMetadataClient{},
	}

	err := f.populateChildren([]string{})
	require.NoError(t, err)
	assert.Empty(t, f.children)
}

func TestPopulateChildrenWithError(t *testing.T) {
	// Mock client that returns an error for Stat
	client := &mockMetadataClient{
		getFunc: func(_ string) (string, error) {
			return "", errors.New("stat error")
		},
	}

	f := &gcpmetaFile{
		ctx:    t.Context(),
		name:   "test",
		client: client,
		root:   "test",
	}

	err := f.populateChildren([]string{"child1"})
	require.Error(t, err)
}

func TestFetchDirectoryListingWithTokenError(t *testing.T) {
	// Mock client that returns a token error
	client := &mockMetadataClient{
		getFunc: func(_ string) (string, error) {
			return "", &metadata.Error{
				Code:    http.StatusBadRequest,
				Message: "non-empty audience parameter required",
			}
		},
	}

	f := &gcpmetaFile{
		ctx:    t.Context(),
		name:   "test",
		client: client,
	}

	children, err := f.fetchDirectoryListing("test")
	require.NoError(t, err)
	assert.Empty(t, children)
}

func TestSubWithEmptyPath(t *testing.T) {
	fs := &gcpmetaFS{
		ctx:        t.Context(),
		metaclient: &mockMetadataClient{},
		root:       "instance",
	}
	subFS, err := fs.Sub(".")
	require.NoError(t, err)
	assert.NotNil(t, subFS)

	// Check that the root was not changed
	gcpFS, ok := subFS.(*gcpmetaFS)
	assert.True(t, ok)
	assert.Equal(t, "instance", gcpFS.root)
}

func TestReadDirWithInvalidPath(t *testing.T) {
	fs := &gcpmetaFS{
		ctx:        t.Context(),
		metaclient: &mockMetadataClient{},
		root:       "",
	}

	// Test with a path that will cause list to return an error
	client := &errorMetadataClient{err: "list error"}
	fs.metaclient = client

	_, err := fs.ReadDir("error-path")
	require.Error(t, err)
}

func TestPopulateChildrenWithIsDir(t *testing.T) {
	f := &gcpmetaFile{
		ctx:    t.Context(),
		name:   "",
		root:   "",
		client: &mockMetadataClient{},
	}

	// Use a known metadata directory path
	err := f.populateChildren([]string{"instance"})
	require.NoError(t, err)
	assert.Len(t, f.children, 1)
	assert.True(t, f.children[0].fi.IsDir())
}

func TestPopulateChildrenWithNotExist(t *testing.T) {
	// Mock client that returns ErrNotExist for Stat
	client := &mockMetadataClient{
		getFunc: func(_ string) (string, error) {
			return "", fs.ErrNotExist
		},
	}

	f := &gcpmetaFile{
		ctx:    t.Context(),
		name:   "test",
		client: client,
		root:   "test",
	}

	// This should skip the child that doesn't exist
	err := f.populateChildren([]string{"nonexistent"})
	require.NoError(t, err)
	assert.Empty(t, f.children)
}

func TestPopulateChildrenWithDuplicates(t *testing.T) {
	f := &gcpmetaFile{
		ctx:    t.Context(),
		name:   "",
		root:   "",
		client: &mockMetadataClient{},
	}

	// Add duplicate entries
	err := f.populateChildren([]string{"instance", "instance"})
	require.NoError(t, err)
	assert.Len(t, f.children, 1) // Should only have one entry despite duplicates
}

func TestIsRootOrMainCategory(t *testing.T) {
	assert.True(t, isRootOrMainCategory(""))
}

func TestWithMetadataClientWithNil(t *testing.T) {
	fsys := &gcpmetaFS{}
	newFS := fsys.WithMetadataClient(nil)
	assert.Equal(t, fsys, newFS)
}

func TestListPrefixWithRootDir(t *testing.T) {
	f := &gcpmetaFile{
		name: ".",
		root: "",
	}
	assert.Empty(t, f.listPrefix())
}

func TestListWithError(t *testing.T) {
	// Mock client that returns an error
	client := &mockMetadataClient{
		getFunc: func(_ string) (string, error) {
			return "", errors.New("list error")
		},
	}

	f := &gcpmetaFile{
		ctx:    t.Context(),
		name:   "error-dir",
		client: client,
	}

	err := f.list()
	require.Error(t, err)
}

func TestListWithGuestAttributesError(t *testing.T) {
	// Mock client that returns a guest attributes error
	client := &mockMetadataClient{
		getFunc: func(_ string) (string, error) {
			return "", &metadata.Error{
				Code:    http.StatusForbidden,
				Message: "Guest attributes endpoint access is disabled",
			}
		},
	}

	f := &gcpmetaFile{
		ctx:    t.Context(),
		name:   "guest-attributes",
		client: client,
	}

	err := f.list()
	require.NoError(t, err)
	assert.Empty(t, f.children)
}

func TestReadDirWithInvalidPathFormat(t *testing.T) {
	fs := &gcpmetaFS{
		ctx:        t.Context(),
		metaclient: &mockMetadataClient{},
		root:       "",
	}

	// Test with an invalid path format
	_, err := fs.ReadDir("../invalid")
	require.Error(t, err)
}
