package fsimpl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/hairyhenderson/go-fsimpl/internal/env"
	"gocloud.dev/blob"
	"gocloud.dev/blob/gcsblob"
	"gocloud.dev/blob/s3blob"
	"gocloud.dev/gcerrors"
	"gocloud.dev/gcp"
)

type blobFS struct {
	ctx     context.Context
	base    *url.URL
	hclient *http.Client
	bucket  *blob.Bucket
	root    string
}

// Some blob APIs don't return valid modTimes, and some do. To conform to fstest
// set this to a fake value
//nolint:gochecknoglobals
var fakeModTime *time.Time

// BlobFS provides a file system (an fs.FS) backed by an blob storage bucket,
// rooted at the given URL.
//
// A context can be given by using WithContextFS.
func BlobFS(base *url.URL) (fs.FS, error) {
	switch base.Scheme {
	case schemeS3, schemeGCS:
	default:
		return nil, fmt.Errorf("invalid URL scheme %q", base.Scheme)
	}

	root := strings.TrimPrefix(base.Path, "/")

	return &blobFS{
		ctx:     context.Background(),
		base:    base,
		hclient: http.DefaultClient,
		root:    root,
	}, nil
}

var (
	_ fs.FS            = (*blobFS)(nil)
	_ fs.ReadFileFS    = (*blobFS)(nil)
	_ fs.SubFS         = (*blobFS)(nil)
	_ withContexter    = (*blobFS)(nil)
	_ withHTTPClienter = (*blobFS)(nil)
)

func (f blobFS) WithContext(ctx context.Context) fs.FS {
	fsys := f
	fsys.ctx = ctx

	return &fsys
}

func (f blobFS) WithHTTPClient(client *http.Client) fs.FS {
	fsys := f
	fsys.hclient = client

	return &fsys
}

func (f *blobFS) openBucket() (*blob.Bucket, error) {
	o, err := f.newOpener(f.ctx, f.base.Scheme)
	if err != nil {
		return nil, fmt.Errorf("bucket opener: %w", err)
	}

	u := cleanCdkURL(*f.base)

	bucket, err := o.OpenBucketURL(f.ctx, &u)
	if err != nil {
		return nil, fmt.Errorf("open bucket: %w", err)
	}

	return bucket, nil
}

func (f *blobFS) Sub(name string) (fs.FS, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "sub", Path: name, Err: fs.ErrInvalid}
	}

	if name == "." || name == "" {
		return f, nil
	}

	fsys := *f
	fsys.root = path.Join(fsys.root, name)

	return &fsys, nil
}

func (f *blobFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}

	if f.bucket == nil {
		bucket, err := f.openBucket()
		if err != nil {
			return nil, fmt.Errorf("open bucket: %w", err)
		}

		f.bucket = bucket
	}

	file := &blobFile{
		ctx:       f.ctx,
		name:      strings.TrimPrefix(path.Base(name), "."),
		bucket:    f.bucket,
		root:      strings.TrimPrefix(path.Join(f.root, path.Dir(name)), "."),
		pageToken: blob.FirstPageToken,
	}

	if name == "." {
		fi := createDirInfo(file.name)

		if fakeModTime != nil {
			fi.modTime = *fakeModTime
		}

		file.fi = &fi

		return file, nil
	}

	_, err := file.Stat()
	if err != nil {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: err}
	}

	return file, nil
}

func (f *blobFS) ReadFile(name string) ([]byte, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readFile", Path: name, Err: fs.ErrInvalid}
	}

	if f.bucket == nil {
		bucket, err := f.openBucket()
		if err != nil {
			return nil, fmt.Errorf("open bucket: %w", err)
		}

		f.bucket = bucket
	}

	return f.bucket.ReadAll(f.ctx, path.Join(f.root, name))
}

// create the correct kind of blob.BucketURLOpener for the given scheme
func (f *blobFS) newOpener(ctx context.Context, scheme string) (opener blob.BucketURLOpener, err error) {
	switch scheme {
	case schemeS3:
		sess := f.initS3Session()

		// see https://gocloud.dev/concepts/urls/#muxes
		return &s3blob.URLOpener{ConfigProvider: sess}, nil
	case schemeGCS:
		if env.Getenv("GOOGLE_ANON") == "true" {
			return &gcsblob.URLOpener{
				Client: gcp.NewAnonymousHTTPClient(f.hclient.Transport),
			}, nil
		}

		creds, err := gcp.DefaultCredentials(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve GCP credentials: %w", err)
		}

		client, err := gcp.NewHTTPClient(
			f.hclient.Transport,
			gcp.CredentialsTokenSource(creds))
		if err != nil {
			return nil, fmt.Errorf("failed to create GCP HTTP client: %w", err)
		}

		return &gcsblob.URLOpener{Client: client}, nil
	default:
		return nil, fmt.Errorf("unsupported scheme: %s", scheme)
	}
}

// initS3Session -
func (f *blobFS) initS3Session() *session.Session {
	config := aws.NewConfig()
	config = config.WithHTTPClient(f.hclient)

	if env.Getenv("AWS_ANON") == "true" {
		config = config.WithCredentials(credentials.AnonymousCredentials)
	}

	config = config.WithCredentialsChainVerboseErrors(true)

	return session.Must(session.NewSessionWithOptions(session.Options{
		Config:            *config,
		SharedConfigState: session.SharedConfigEnable,
	}))
}

// copy/sanitize the URL for the Go CDK - it doesn't like params it can't parse
func cleanCdkURL(u url.URL) url.URL {
	switch u.Scheme {
	case "s3":
		return cleanS3URL(u)
	case "gs":
		return cleanGSURL(u)
	default:
		return u
	}
}

func cleanGSURL(u url.URL) url.URL {
	q := u.Query()
	for param := range q {
		switch param {
		case "access_id", "private_key_path":
		default:
			q.Del(param)
		}
	}

	u.RawQuery = q.Encode()

	return u
}

func cleanS3URL(u url.URL) url.URL {
	q := u.Query()
	for param := range q {
		switch param {
		case "region", "endpoint", "disableSSL", "s3ForcePathStyle":
		default:
			q.Del(param)
		}
	}

	if q.Get("endpoint") == "" {
		endpoint := env.Getenv("AWS_S3_ENDPOINT")
		if endpoint != "" {
			q.Set("endpoint", endpoint)
		}
	}

	if q.Get("region") == "" {
		region := env.Getenv("AWS_REGION", env.Getenv("AWS_DEFAULT_REGION"))
		if region != "" {
			q.Set("region", region)
		}
	}

	u.RawQuery = q.Encode()

	return u
}

type blobFile struct {
	ctx       context.Context
	reader    *blob.Reader
	bucket    *blob.Bucket
	fi        *staticFileInfo
	listIter  *blob.ListIterator
	name      string
	root      string
	pageToken []byte
}

var _ fs.ReadDirFile = (*blobFile)(nil)

func (f *blobFile) Close() error {
	if f.reader == nil {
		return nil
	}

	return f.reader.Close()
}

func (f *blobFile) Read(p []byte) (int, error) {
	if f.reader == nil {
		r, err := f.bucket.NewReader(f.ctx, path.Join(f.root, f.name), nil)
		if err != nil {
			return 0, &fs.PathError{Op: "read", Path: f.name, Err: err}
		}

		f.reader = r
	}

	return f.reader.Read(p)
}

func (f *blobFile) Stat() (fs.FileInfo, error) {
	if f.fi != nil {
		return f.fi, nil
	}

	out, err := f.bucket.Attributes(f.ctx, path.Join(f.root, f.name))
	if gcerrors.Code(err) == gcerrors.NotFound {
		return blobFindDir(f.ctx, f.bucket, f.root, f.name)
	}

	if err != nil {
		return nil, err
	}

	fi := createFileInfo(f.name, out.Size, 0o644, out.ModTime, out.ContentType)

	if fakeModTime != nil {
		fi.modTime = *fakeModTime
	}

	f.fi = &fi

	return &fi, nil
}

func blobFindDir(ctx context.Context, bucket *blob.Bucket, root, name string) (fs.FileInfo, error) {
	// Prefix is not suffixed with /, so that we find only a single entry, if a
	// dir with that name exists
	opts := blob.ListOptions{Delimiter: "/", Prefix: path.Join(root, name)}

	list, _, err := bucket.ListPage(ctx, blob.FirstPageToken, 1, &opts)
	if err != nil {
		return nil, err
	}

	if len(list) != 1 {
		return nil, fs.ErrNotExist
	}

	dir := list[0]

	if !dir.IsDir {
		return nil, fmt.Errorf("not a directory: %s", name)
	}

	fi := createDirInfo(path.Base(name))

	if fakeModTime != nil {
		fi.modTime = *fakeModTime
	}

	return &fi, nil
}

//nolint:gocyclo
func (f *blobFile) ReadDir(n int) ([]fs.DirEntry, error) {
	if f.listIter == nil {
		opts := blob.ListOptions{Delimiter: "/", Prefix: path.Join(f.root, f.name)}
		if opts.Prefix != "" {
			opts.Prefix += "/"
		}

		f.listIter = f.bucket.List(&opts)
	}

	dirents := []fs.DirEntry{}

	for i := 0; (n > 0 && i < n) || n <= 0; i++ {
		obj, err := f.listIter.Next(f.ctx)
		if errors.Is(err, io.EOF) {
			if n <= 0 {
				err = nil
			}

			return dirents, err
		}

		if err != nil {
			return nil, err
		}

		mode := fs.FileMode(0o644)
		if obj.IsDir {
			mode = fs.ModeDir
		}

		name := strings.TrimSuffix(path.Base(obj.Key), "/")
		dirent := createFileInfo(name, obj.Size, mode, obj.ModTime, "")

		if fakeModTime != nil {
			dirent.modTime = *fakeModTime
		}

		dirents = append(dirents, &dirent)
	}

	return dirents, nil
}

func createFileInfo(name string, size int64, mode fs.FileMode, modTime time.Time, contentType string) staticFileInfo {
	return staticFileInfo{
		name:        name,
		size:        size,
		mode:        mode,
		modTime:     modTime,
		contentType: contentType,
	}
}

func createDirInfo(name string) staticFileInfo {
	return staticFileInfo{name: name, mode: fs.ModeDir}
}
