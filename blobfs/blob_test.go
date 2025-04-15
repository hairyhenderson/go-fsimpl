package blobfs

import (
	"bytes"
	"context"
	"io/fs"
	"net/http/httptest"
	"net/url"
	"testing"
	"testing/fstest"
	"time"

	"github.com/fsouza/fake-gcs-server/fakestorage"
	"github.com/hairyhenderson/go-fsimpl"
	"github.com/hairyhenderson/go-fsimpl/awsimdsfs"
	"github.com/hairyhenderson/go-fsimpl/internal/tests"
	"github.com/hairyhenderson/go-fsimpl/internal/tests/fakeimds"
	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestS3Bucket(t *testing.T) *url.URL {
	t.Helper()

	backend := s3mem.New()
	faker := gofakes3.New(backend)

	srv := httptest.NewServer(faker.Server())

	t.Cleanup(srv.Close)

	require.NoError(t, backend.CreateBucket("mybucket"))
	require.NoError(t, putFile(backend, "file1", "text/plain", "hello"))
	require.NoError(t, putFile(backend, "file2", "application/json", `{"value": "goodbye world"}`))
	require.NoError(t, putFile(backend, "file3", "application/yaml", `value: what a world`))
	require.NoError(t, putFile(backend, "dir1/file1", "application/yaml", `value: out of this world`))
	require.NoError(t, putFile(backend, "dir1/file2", "application/yaml", `value: foo`))
	require.NoError(t, putFile(backend, "dir2/file3", "text/plain", "foo"))
	require.NoError(t, putFile(backend, "dir2/file4", "text/plain", "bar"))
	require.NoError(t, putFile(backend, "dir2/sub1/subfile1", "text/plain", "baz"))
	require.NoError(t, putFile(backend, "dir2/sub1/subfile2", "text/plain", "qux"))

	return tests.MustURL(srv.URL)
}

func fakeGCSObject(name, contentType, content string) fakestorage.Object {
	return fakestorage.Object{
		ObjectAttrs: fakestorage.ObjectAttrs{BucketName: "mybucket", Name: name, ContentType: contentType},
		Content:     []byte(content),
	}
}

func setupTestGCSBucket(t *testing.T) *fakestorage.Server {
	t.Helper()

	objs := []fakestorage.Object{
		fakeGCSObject("file1", "text/plain", "hello"),
		fakeGCSObject("file2", "application/json", `{"value": "goodbye world"}`),
		fakeGCSObject("file3", "application/yaml", `value: what a world`),
		fakeGCSObject("dir1/file1", "application/yaml", `value: out of this world`),
		fakeGCSObject("dir1/file2", "application/yaml", `value: foo`),
		fakeGCSObject("dir2/file3", "text/plain", "foo"),
		fakeGCSObject("dir2/file4", "text/plain", "bar"),
		fakeGCSObject("dir2/sub1/subfile1", "text/plain", "baz"),
		fakeGCSObject("dir2/sub1/subfile2", "text/plain", "qux"),
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
	// override timestamps to make tests deterministic
	ft := time.Now()
	fakeModTime = &ft

	t.Cleanup(func() { fakeModTime = nil })

	srvURL := setupTestS3Bucket(t)
	_, u := fakeimds.Server(t)
	imdsfsys, err := awsimdsfs.New(u)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	t.Run("anon with region from URL", func(t *testing.T) {
		t.Setenv("AWS_ANON", "true")

		fsys, err := New(tests.MustURL("s3://mybucket/?region=us-east-1&disableSSL=true&use_path_style=true&endpoint=" + srvURL.Host))
		require.NoError(t, err)

		require.NoError(t, fstest.TestFS(fsimpl.WithContextFS(ctx, fsys),
			"file1", "file2", "file3", "dir1/file1", "dir1/file2",
			"dir2/file3", "dir2/file4", "dir2/sub1/subfile1", "dir2/sub1/subfile2"),
		)
	})

	t.Run("auth with region from URL and subdir", func(t *testing.T) {
		t.Setenv("AWS_ACCESS_KEY_ID", "fake")
		t.Setenv("AWS_SECRET_ACCESS_KEY", "fake")
		t.Setenv("AWS_S3_ENDPOINT", srvURL.Host)
		t.Setenv("AWS_REGION", "eu-west-1")

		fsys, err := New(tests.MustURL("s3://mybucket/dir2/?disableSSL=true&use_path_style=true"))
		require.NoError(t, err)

		require.NoError(t, fstest.TestFS(fsimpl.WithContextFS(ctx, fsys),
			"file3", "file4", "sub1/subfile1", "sub1/subfile2"))
	})

	t.Run("auth with region from IMDS and subdir", func(t *testing.T) {
		t.Setenv("AWS_ACCESS_KEY_ID", "fake")
		t.Setenv("AWS_SECRET_ACCESS_KEY", "fake")
		t.Setenv("AWS_S3_ENDPOINT", srvURL.Host)

		fsys, err := New(tests.MustURL("s3://mybucket/dir2/?disableSSL=true&use_path_style=true"))
		require.NoError(t, err)

		fsys = fsys.(*blobFS).WithIMDSFS(imdsfsys)

		require.NoError(t, fstest.TestFS(fsimpl.WithContextFS(ctx, fsys),
			"file3", "file4", "sub1/subfile1", "sub1/subfile2"))
	})
}

func TestBlobFS_GCS(t *testing.T) {
	ft := time.Now()
	fakeModTime = &ft

	defer func() { fakeModTime = nil }()

	srv := setupTestGCSBucket(t)

	t.Setenv("GOOGLE_ANON", "true")

	fsys, err := New(tests.MustURL("gs://mybucket"))
	require.NoError(t, err)

	fsys = fsimpl.WithHTTPClientFS(srv.HTTPClient(), fsys)

	require.NoError(t, fstest.TestFS(fsys,
		"file1", "file2", "file3",
		"dir1/file1", "dir1/file2",
		"dir2/file3", "dir2/file4",
		"dir2/sub1/subfile1", "dir2/sub1/subfile2"),
	)

	fsys, err = New(tests.MustURL("gs://mybucket/dir2/"))
	require.NoError(t, err)

	fsys = fsimpl.WithHTTPClientFS(srv.HTTPClient(), fsys)

	require.NoError(t, fstest.TestFS(fsys,
		"file3", "file4", "sub1/subfile1", "sub1/subfile2"))
}

func TestBlobFS_Azure(t *testing.T) {
	t.Skip("Only run this locally for now")

	ft := time.Now()
	fakeModTime = &ft

	defer func() { fakeModTime = nil }()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	t.Setenv("AZURE_STORAGE_ACCOUNT", "azureopendatastorage")

	fsys, err := New(tests.MustURL("azblob://citydatacontainer/Crime/Processed/2020/1/20/"))
	require.NoError(t, err)

	fsys = fsimpl.WithContextFS(ctx, fsys)

	des, err := fs.ReadDir(fsys, ".")
	require.NoError(t, err)

	t.Logf("entries: %d", len(des))

	for _, de := range des {
		if de.IsDir() {
			t.Logf("%s/", de.Name())
		} else {
			fi, err := de.Info()
			require.NoError(t, err)

			t.Logf("%s - %d - %v", de.Name(), fi.Size(), fi.ModTime())
		}
	}

	t.Fail()

	require.NoError(t, fstest.TestFS(fsys,
		"Boston", "Chicago", "NewYorkCity", "SanFrancisco", "Seattle"))
}

func TestBlobFS_ReadDir(t *testing.T) {
	srvURL := setupTestS3Bucket(t)

	t.Setenv("AWS_ANON", "true")

	fsys, err := New(tests.MustURL("s3://mybucket/?region=us-east-1&disableSSL=true&use_path_style=true&endpoint=" + srvURL.Host))
	require.NoError(t, err)

	de, err := fs.ReadDir(fsys, "dir1")
	require.NoError(t, err)
	assert.Len(t, de, 2)

	de, err = fs.ReadDir(fsys, ".")
	require.NoError(t, err)
	assert.Len(t, de, 5)

	fi, err := de[0].Info()
	require.NoError(t, err)
	assert.Equal(t, "dir1", fi.Name())

	f, err := fsys.Open("dir1")
	require.NoError(t, err)
	assert.IsType(t, &blobFile{}, f)

	fi, err = f.Stat()
	require.NoError(t, err)
	assert.Equal(t, "dir1", fi.Name())

	f, err = fsys.Open("file1")
	require.NoError(t, err)

	defer f.Close()

	fi, err = f.Stat()
	require.NoError(t, err)
	assert.Equal(t, fs.FileMode(0o444), fi.Mode())
}

func TestBlobFS_CleanCdkURL(t *testing.T) {
	b := &blobFS{}

	data := []struct {
		in, expected string
	}{
		{"s3://foo/bar/baz", "s3://foo/bar/baz"},
		{"s3://foo/bar/baz?type=hello/world", "s3://foo/bar/baz"},
		{"s3://foo/bar/baz?region=us-east-1", "s3://foo/bar/baz?region=us-east-1"},
		{"s3://foo/bar/baz?disableSSL=true&type=text/csv", "s3://foo/bar/baz?disable_https=true"},
		{
			"s3://foo/bar/baz?type=text/csv&use_path_style=true&endpoint=1.2.3.4",
			"s3://foo/bar/baz?endpoint=https%3A%2F%2F1.2.3.4&use_path_style=true",
		},
		{"s3://foo/bar/baz?disable_https=true&type=text/csv", "s3://foo/bar/baz?disable_https=true"},
		{
			"s3://foo/bar/baz?type=text/csv&s3ForcePathStyle=true&endpoint=1.2.3.4",
			"s3://foo/bar/baz?endpoint=https%3A%2F%2F1.2.3.4&s3ForcePathStyle=true",
		},
		{
			"s3://foo/bar/baz?disable_https=true&type=text/csv&use_path_style=true&endpoint=1.2.3.4:1234",
			"s3://foo/bar/baz?disable_https=true&endpoint=http%3A%2F%2F1.2.3.4%3A1234&use_path_style=true",
		},
		{"gs://foo/bar/baz", "gs://foo/bar/baz"},
		{"gs://foo/bar/baz?type=foo/bar", "gs://foo/bar/baz"},
		{"gs://foo/bar/baz?access_id=123", "gs://foo/bar/baz?access_id=123"},
		{"gs://foo/bar/baz?private_key_path=/foo/bar", "gs://foo/bar/baz?private_key_path=%2Ffoo%2Fbar"},
		{"gs://foo/bar/baz?private_key_path=key.json&foo=bar", "gs://foo/bar/baz?private_key_path=key.json"},
		{"gs://foo/bar/baz?private_key_path=key.json&foo=bar&access_id=abcd", "gs://foo/bar/baz?access_id=abcd&private_key_path=key.json"},
	}

	for _, d := range data {
		u := tests.MustURL(d.in)
		expected := tests.MustURL(d.expected)
		assert.Equal(t, *expected, b.cleanCdkURL(*u))
	}
}
