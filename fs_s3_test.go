package fsimpl

import (
	"bytes"
	"context"
	"io/fs"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"testing/fstest"
	"time"

	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"
	"github.com/stretchr/testify/assert"
)

func setupTestBucket(t *testing.T) *url.URL {
	backend := s3mem.New()
	faker := gofakes3.New(backend,
		gofakes3.WithTimeSource(gofakes3.FixedTimeSource(time.Time{})),
	)

	srv := httptest.NewServer(faker.Server())

	t.Cleanup(srv.Close)

	assert.NoError(t, backend.CreateBucket("mybucket"))
	assert.NoError(t, putFile(backend, "file1", "text/plain", "hello"))
	assert.NoError(t, putFile(backend, "file2", "application/json", `{"value": "goodbye world"}`))
	assert.NoError(t, putFile(backend, "file3", "application/yaml", `value: what a world`))
	assert.NoError(t, putFile(backend, "dir1/file1", "application/yaml", `value: out of this world`))
	assert.NoError(t, putFile(backend, "dir1/file2", "application/yaml", `value: foo`))
	assert.NoError(t, putFile(backend, "dir2/file3", "text/plain", "foo"))
	assert.NoError(t, putFile(backend, "dir2/file4", "text/plain", "bar"))
	assert.NoError(t, putFile(backend, "dir2/sub1/subfile1", "text/plain", "baz"))
	assert.NoError(t, putFile(backend, "dir2/sub1/subfile2", "text/plain", "qux"))

	u, _ := url.Parse(srv.URL)

	return u
}

func putFile(backend gofakes3.Backend, file, mime, content string) error {
	_, err := backend.PutObject(
		"mybucket",
		file,
		map[string]string{"Content-Type": mime},
		bytes.NewBufferString(content),
		int64(len(content)),
	)

	return err
}

func TestS3FS(t *testing.T) {
	srvURL := setupTestBucket(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)

	defer cancel()

	os.Setenv("AWS_ANON", "true")
	defer os.Unsetenv("AWS_ANON")

	u, _ := url.Parse("s3://mybucket/?region=us-east-1&disableSSL=true&s3ForcePathStyle=true&type=text/plain&endpoint=" + srvURL.Host)
	// u, _ := url.Parse("s3://ryft-public-sample-data/?region=us-east-1")
	fsys, err := S3FS(u)
	assert.NoError(t, err)

	fsys = WithContextFS(ctx, fsys)
	assert.NoError(t, fstest.TestFS(fsys,
		"AWS-x86-AMI-queries.json",
	))
	assert.NoError(t, fstest.TestFS(fsys,
		"file1", "file2", "file3",
		"dir1/file1", "dir1/file2",
		"dir2/file3", "dir2/file4",
		"dir2/sub1/subfile1", "dir2/sub1/subfile2"),
	)

	// d, err := NewData([]string{"-d",
	// "data=s3://mybucket/file1?region=us-east-1&disableSSL=true&s3ForcePathStyle=true&type=text/plain&endpoint="
	// + u.Host}, nil)
	// assert.NoError(t, err)
	//
	// var expected interface{}
	// expected = "hello"
	// out, err := d.Datasource("data")
	// assert.NoError(t, err)
	// assert.Equal(t, expected, out)
	//
	// os.Unsetenv("AWS_ANON")
	//
	// os.Setenv("AWS_ACCESS_KEY_ID", "fake")
	// os.Setenv("AWS_SECRET_ACCESS_KEY", "fake")
	// defer os.Unsetenv("AWS_ACCESS_KEY_ID")
	// defer os.Unsetenv("AWS_SECRET_ACCESS_KEY")
	// os.Setenv("AWS_S3_ENDPOINT", u.Host)
	// defer os.Unsetenv("AWS_S3_ENDPOINT")
	//
	// d, err = NewData([]string{"-d", "data=s3://mybucket/file2?region=us-east-1&disableSSL=true&s3ForcePathStyle=true"}, nil)
	// assert.NoError(t, err)
	//
	// expected = map[string]interface{}{"value": "goodbye world"}
	// out, err = d.Datasource("data")
	// assert.NoError(t, err)
	// assert.Equal(t, expected, out)
	//
	// d, err = NewData([]string{"-d", "data=s3://mybucket/?region=us-east-1&disableSSL=true&s3ForcePathStyle=true"}, nil)
	// assert.NoError(t, err)
	//
	// expected = []interface{}{"dir1/", "file1", "file2", "file3"}
	// out, err = d.Datasource("data")
	// assert.NoError(t, err)
	// assert.EqualValues(t, expected, out)
	//
	// d, err = NewData([]string{"-d", "data=s3://mybucket/dir1/?region=us-east-1&disableSSL=true&s3ForcePathStyle=true"}, nil)
	// assert.NoError(t, err)
	//
	// expected = []interface{}{"file1", "file2"}
	// out, err = d.Datasource("data")
	// assert.NoError(t, err)
	// assert.EqualValues(t, expected, out)
}

func TestS3FS_ReadDir(t *testing.T) {
	srvURL := setupTestBucket(t)

	os.Setenv("AWS_ANON", "true")
	defer os.Unsetenv("AWS_ANON")

	u, _ := url.Parse("s3://mybucket/?region=us-east-1&disableSSL=true&s3ForcePathStyle=true&type=text/plain&endpoint=" + srvURL.Host)
	fsys, err := S3FS(u)
	assert.NoError(t, err)

	de, err := fs.ReadDir(fsys, "dir1")
	assert.NoError(t, err)
	assert.Len(t, de, 2)

	de, err = fs.ReadDir(fsys, ".")
	assert.NoError(t, err)
	assert.Len(t, de, 5)

	f, err := fsys.Open("file1")
	assert.NoError(t, err)

	defer f.Close()

	fi, err := f.Stat()
	assert.NoError(t, err)
	assert.Equal(t, fs.FileMode(0o644), fi.Mode())
}

func TestS3FS_CleanCdkURL(t *testing.T) {
	data := []struct {
		in, expected string
	}{
		{"s3://foo/bar/baz", "s3://foo/bar/baz"},
		{"s3://foo/bar/baz?type=hello/world", "s3://foo/bar/baz"},
		{"s3://foo/bar/baz?region=us-east-1", "s3://foo/bar/baz?region=us-east-1"},
		{"s3://foo/bar/baz?disableSSL=true&type=text/csv", "s3://foo/bar/baz?disableSSL=true"},
		{"s3://foo/bar/baz?type=text/csv&s3ForcePathStyle=true&endpoint=1.2.3.4", "s3://foo/bar/baz?endpoint=1.2.3.4&s3ForcePathStyle=true"},
		{"gs://foo/bar/baz", "gs://foo/bar/baz"},
		{"gs://foo/bar/baz?type=foo/bar", "gs://foo/bar/baz"},
		{"gs://foo/bar/baz?access_id=123", "gs://foo/bar/baz?access_id=123"},
		{"gs://foo/bar/baz?private_key_path=/foo/bar", "gs://foo/bar/baz?private_key_path=%2Ffoo%2Fbar"},
		{"gs://foo/bar/baz?private_key_path=key.json&foo=bar", "gs://foo/bar/baz?private_key_path=key.json"},
		{"gs://foo/bar/baz?private_key_path=key.json&foo=bar&access_id=abcd", "gs://foo/bar/baz?access_id=abcd&private_key_path=key.json"},
	}

	for _, d := range data {
		u, _ := url.Parse(d.in)
		assert.Equal(t, d.expected, cleanCdkURL(*u))
	}
}
