package blobfs

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

	"github.com/fsouza/fake-gcs-server/fakestorage"
	"github.com/hairyhenderson/go-fsimpl"
	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"
	"github.com/stretchr/testify/assert"
)

func setupTestS3Bucket(t *testing.T) *url.URL {
	backend := s3mem.New()
	faker := gofakes3.New(backend)

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

func setupTestGCSBucket(t *testing.T) *fakestorage.Server {
	objs := []fakestorage.Object{
		{BucketName: "mybucket", Name: "file1", ContentType: "text/plain", Content: []byte("hello")},
		{BucketName: "mybucket", Name: "file2", ContentType: "application/json", Content: []byte(`{"value": "goodbye world"}`)},
		{BucketName: "mybucket", Name: "file3", ContentType: "application/yaml", Content: []byte(`value: what a world`)},
		{BucketName: "mybucket", Name: "dir1/file1", ContentType: "application/yaml", Content: []byte(`value: out of this world`)},
		{BucketName: "mybucket", Name: "dir1/file2", ContentType: "application/yaml", Content: []byte(`value: foo`)},
		{BucketName: "mybucket", Name: "dir2/file3", ContentType: "text/plain", Content: []byte("foo")},
		{BucketName: "mybucket", Name: "dir2/file4", ContentType: "text/plain", Content: []byte("bar")},
		{BucketName: "mybucket", Name: "dir2/sub1/subfile1", ContentType: "text/plain", Content: []byte("baz")},
		{BucketName: "mybucket", Name: "dir2/sub1/subfile2", ContentType: "text/plain", Content: []byte("qux")},
	}

	srv, err := fakestorage.NewServerWithOptions(fakestorage.Options{
		InitialObjects: objs,
		Scheme:         "http",
		Host:           "127.0.0.1",
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(srv.Stop)

	return srv
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

func TestBlobFS_S3(t *testing.T) {
	ft := time.Now()
	fakeModTime = &ft

	defer func() { fakeModTime = nil }()

	srvURL := setupTestS3Bucket(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	defer cancel()

	os.Setenv("AWS_ANON", "true")
	defer os.Unsetenv("AWS_ANON")

	u, _ := url.Parse("s3://mybucket/?region=us-east-1&disableSSL=true&s3ForcePathStyle=true&endpoint=" + srvURL.Host)
	fsys, err := New(u)
	assert.NoError(t, err)

	assert.NoError(t, fstest.TestFS(fsimpl.WithContextFS(ctx, fsys),
		"file1", "file2", "file3",
		"dir1/file1", "dir1/file2",
		"dir2/file3", "dir2/file4",
		"dir2/sub1/subfile1", "dir2/sub1/subfile2"),
	)

	os.Unsetenv("AWS_ANON")

	os.Setenv("AWS_ACCESS_KEY_ID", "fake")
	defer os.Unsetenv("AWS_ACCESS_KEY_ID")

	os.Setenv("AWS_SECRET_ACCESS_KEY", "fake")
	defer os.Unsetenv("AWS_SECRET_ACCESS_KEY")

	os.Setenv("AWS_S3_ENDPOINT", srvURL.Host)
	defer os.Unsetenv("AWS_S3_ENDPOINT")

	os.Setenv("AWS_REGION", "eu-west-1")
	defer os.Unsetenv("AWS_REGION")

	u, _ = url.Parse("s3://mybucket/dir2/?disableSSL=true&s3ForcePathStyle=true")
	fsys, err = New(u)
	assert.NoError(t, err)

	assert.NoError(t, fstest.TestFS(fsimpl.WithContextFS(ctx, fsys),
		"file3", "file4", "sub1/subfile1", "sub1/subfile2"))
}

func TestBlobFS_GCS(t *testing.T) {
	ft := time.Now()
	fakeModTime = &ft

	defer func() { fakeModTime = nil }()

	srv := setupTestGCSBucket(t)

	os.Setenv("GOOGLE_ANON", "true")
	defer os.Unsetenv("GOOGLE_ANON")

	u, _ := url.Parse("gs://mybucket")
	fsys, err := New(u)
	assert.NoError(t, err)

	fsys = fsimpl.WithHTTPClientFS(srv.HTTPClient(), fsys)

	assert.NoError(t, fstest.TestFS(fsys,
		"file1", "file2", "file3",
		"dir1/file1", "dir1/file2",
		"dir2/file3", "dir2/file4",
		"dir2/sub1/subfile1", "dir2/sub1/subfile2"),
	)

	u, _ = url.Parse("gs://mybucket/dir2/")
	fsys, err = New(u)
	assert.NoError(t, err)

	fsys = fsimpl.WithHTTPClientFS(srv.HTTPClient(), fsys)

	assert.NoError(t, fstest.TestFS(fsys,
		"file3", "file4", "sub1/subfile1", "sub1/subfile2"))
}

func TestBlobFS_Azure(t *testing.T) {
	t.Skip("Only run this locally for now")

	ft := time.Now()
	fakeModTime = &ft

	defer func() { fakeModTime = nil }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	defer cancel()

	os.Setenv("AZURE_STORAGE_ACCOUNT", "azureopendatastorage")

	u, _ := url.Parse("azblob://citydatacontainer/Crime/Processed/2020/1/20/")
	fsys, err := New(u)
	assert.NoError(t, err)

	fsys = fsimpl.WithContextFS(ctx, fsys)

	des, err := fs.ReadDir(fsys, ".")
	assert.NoError(t, err)

	t.Logf("entries: %d", len(des))

	for _, de := range des {
		if de.IsDir() {
			t.Logf("%s/", de.Name())
		} else {
			fi, err := de.Info()
			assert.NoError(t, err)

			t.Logf("%s - %d - %v", de.Name(), fi.Size(), fi.ModTime())
		}
	}

	t.Fail()

	assert.NoError(t, fstest.TestFS(fsys,
		"Boston", "Chicago", "NewYorkCity", "SanFrancisco", "Seattle"))
}

func TestBlobFS_ReadDir(t *testing.T) {
	srvURL := setupTestS3Bucket(t)

	os.Setenv("AWS_ANON", "true")
	defer os.Unsetenv("AWS_ANON")

	u, _ := url.Parse("s3://mybucket/?region=us-east-1&disableSSL=true&s3ForcePathStyle=true&endpoint=" + srvURL.Host)
	fsys, err := New(u)
	assert.NoError(t, err)

	de, err := fs.ReadDir(fsys, "dir1")
	assert.NoError(t, err)
	assert.Len(t, de, 2)

	de, err = fs.ReadDir(fsys, ".")
	assert.NoError(t, err)
	assert.Len(t, de, 5)

	fi, err := de[0].Info()
	assert.NoError(t, err)
	assert.Equal(t, "dir1", fi.Name())

	f, err := fsys.Open("dir1")
	assert.NoError(t, err)
	assert.IsType(t, &blobFile{}, f)

	fi, err = f.Stat()
	assert.NoError(t, err)
	assert.Equal(t, "dir1", fi.Name())

	f, err = fsys.Open("file1")
	assert.NoError(t, err)

	defer f.Close()

	fi, err = f.Stat()
	assert.NoError(t, err)
	assert.Equal(t, fs.FileMode(0o644), fi.Mode())
}

func TestBlobFS_CleanCdkURL(t *testing.T) {
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
		expected, _ := url.Parse(d.expected)
		assert.Equal(t, *expected, cleanCdkURL(*u))
	}
}
