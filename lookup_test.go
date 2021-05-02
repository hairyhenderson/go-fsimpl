package fsimpl

import (
	"fmt"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
)

func ExampleLookupFS() {
	fsys, _ := LookupFS("file:///somedir")

	list, _ := fs.ReadDir(fsys, ".")

	for _, entry := range list {
		fmt.Printf("found %s\n", entry.Name())
	}

	// Output:
	//
}

func TestLookupFS(t *testing.T) {
	_, err := LookupFS("bad*:url//bogus")
	assert.Error(t, err)

	_, err = LookupFS("unsupported://scheme")
	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), "scheme \"unsupported\"")
	}

	fsys, err := LookupFS("file:///tmp")
	assert.NoError(t, err)
	assert.IsType(t, &fileFS{}, fsys)

	fsys, err = LookupFS("http://example.com/path")
	assert.NoError(t, err)
	assert.IsType(t, &httpFS{}, fsys)
	assert.Equal(t, "example.com", fsys.(*httpFS).base.Host)
	assert.Equal(t, "/path", fsys.(*httpFS).base.Path)

	fsys, err = LookupFS("git+ssh://localhost:1234/foo/bar.git//baz#refs/tags/foo")
	assert.NoError(t, err)
	assert.IsType(t, &gitFS{}, fsys)
	assert.Equal(t, "ssh", fsys.(*gitFS).repo.Scheme)
	assert.Equal(t, "/baz", fsys.(*gitFS).root)

	fsys, err = LookupFS("s3://foo/bar")
	assert.NoError(t, err)
	assert.IsType(t, &blobFS{}, fsys)
	assert.Equal(t, "s3", fsys.(*blobFS).base.Scheme)
	assert.Equal(t, "bar", fsys.(*blobFS).root)

	fsys, err = LookupFS("gs://baz/qux")
	assert.NoError(t, err)
	assert.IsType(t, &blobFS{}, fsys)
	assert.Equal(t, "gs", fsys.(*blobFS).base.Scheme)
	assert.Equal(t, "qux", fsys.(*blobFS).root)

	fsys, err = LookupFS("azblob://quux/corge")
	assert.NoError(t, err)
	assert.IsType(t, &blobFS{}, fsys)
	assert.Equal(t, "azblob", fsys.(*blobFS).base.Scheme)
	assert.Equal(t, "corge", fsys.(*blobFS).root)
}
