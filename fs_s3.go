package fsimpl

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"path"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/hairyhenderson/go-fsimpl/internal/env"
)

type s3FS struct {
	ctx context.Context

	query  url.Values
	bucket string
	root   string

	client *s3.Client
}

// S3FS provides a file system (an fs.FS) backed by an AWS S3 bucket, rooted at
// the given URL.
//
// A context can be given by using WithContextFS.
func S3FS(base *url.URL) (fs.FS, error) {
	if base.Scheme != schemeS3 {
		return nil, fmt.Errorf("invalid URL scheme %q", base.Scheme)
	}

	root := strings.TrimPrefix(base.Path, "/")

	return &s3FS{
		ctx:    context.Background(),
		query:  base.Query(),
		bucket: base.Host,
		root:   root,
	}, nil
}

var (
	_ fs.FS = (*s3FS)(nil)
	// _ fs.ReadFileFS = (*s3FS)(nil)
	// _ fs.SubFS      = (*s3FS)(nil)
	_ withContexter = (*s3FS)(nil)
	// _ withHeaderer  = (*s3FS)(nil)
)

func (f s3FS) WithContext(ctx context.Context) fs.FS {
	fsys := f
	fsys.ctx = ctx

	return &fsys
}

func (f *s3FS) s3Client() (*s3.Client, error) {
	if f.client != nil {
		return f.client, nil
	}

	cfg, err := config.LoadDefaultConfig(f.ctx)
	if err != nil {
		return nil, err
	}

	opts := []func(*s3.Options){}

	if env.Getenv("AWS_ANON") == "true" {
		opts = append(opts, func(opts *s3.Options) {
			opts.Credentials = aws.AnonymousCredentials{}
		})
	}

	// override Region if present in query
	if region := f.query.Get("region"); region != "" {
		opts = append(opts, func(opts *s3.Options) {
			opts.Region = region
		})
	}

	// support s3ForcePathStyle
	if s3ForcePathStyle := f.query.Get("s3ForcePathStyle"); s3ForcePathStyle == "true" {
		opts = append(opts, func(opts *s3.Options) {
			opts.UsePathStyle = true
		})
	}

	// allow disabling HTTPS
	scheme := schemeHTTPS
	if disableSSL := f.query.Get("disableSSL"); disableSSL == "true" {
		scheme = schemeHTTP

		opts = append(opts, func(opts *s3.Options) {
			opts.EndpointOptions.DisableHTTPS = true
		})
	}

	// override endpoint if provided
	if endpoint := f.query.Get("endpoint"); endpoint != "" {
		u := url.URL{Scheme: scheme, Host: endpoint}

		opts = append(opts, func(opts *s3.Options) {
			opts.EndpointResolver = s3.EndpointResolverFromURL(u.String())
		})
	}

	f.client = s3.NewFromConfig(cfg, opts...)

	return f.client, nil
}

func (f *s3FS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}

	c, err := f.s3Client()
	if err != nil {
		return nil, err
	}

	if name == "." {
		name = ""
	}

	return &s3File{
		ctx:    f.ctx,
		bucket: f.bucket,
		name:   name,
		root:   f.root,
		client: c,
	}, nil
}

// copy/sanitize the URL for the Go CDK - it doesn't like params it can't parse
func cleanCdkURL(u url.URL) string {
	out := u
	q := out.Query()

	for param := range q {
		switch u.Scheme {
		case "s3":
			switch param {
			case "region", "endpoint", "disableSSL", "s3ForcePathStyle":
			default:
				q.Del(param)
			}
		case "gs":
			switch param {
			case "access_id", "private_key_path":
			default:
				q.Del(param)
			}
		}
	}

	if u.Scheme == "s3" {
		// handle AWS_S3_ENDPOINT env var
		endpoint := env.Getenv("AWS_S3_ENDPOINT")
		if endpoint != "" {
			q.Set("endpoint", endpoint)
		}
	}

	out.RawQuery = q.Encode()

	return out.String()
}

type s3File struct {
	ctx       context.Context
	body      io.ReadCloser
	contToken *string
	client    *s3.Client
	bucket    string
	name      string
	root      string

	fi *staticFileInfo
}

var _ fs.ReadDirFile = (*s3File)(nil)

func (f *s3File) Close() error {
	if f.body == nil {
		return nil
	}

	return f.body.Close()
}

func (f *s3File) Read(p []byte) (int, error) {
	if f.body == nil {
		key := path.Join(f.root, f.name)
		params := s3.GetObjectInput{
			Bucket: &f.bucket,
			Key:    &key,
		}

		out, err := f.client.GetObject(f.ctx, &params)
		if err != nil {
			return 0, err
		}

		f.fi = &staticFileInfo{
			size: out.ContentLength,
			name: f.name,
			mode: 0o644,
		}

		if out.ContentType != nil {
			f.fi.contentType = *out.ContentType
		}

		if out.LastModified != nil {
			f.fi.modTime = *out.LastModified
		}

		// The response body must be closed later
		f.body = out.Body
	}

	return f.body.Read(p)
}

func (f *s3File) Stat() (fs.FileInfo, error) {
	if f.fi != nil {
		return f.fi, nil
	}

	key := path.Join(f.root, f.name)
	params := s3.HeadObjectInput{
		Bucket: &f.bucket,
		Key:    &key,
	}

	out, err := f.client.HeadObject(f.ctx, &params)
	if err != nil {
		return nil, err
	}

	fi := staticFileInfo{
		size: out.ContentLength,
		name: f.name,
		mode: 0o644,
	}

	if out.ContentType != nil {
		fi.contentType = *out.ContentType
	}

	if out.LastModified != nil {
		fi.modTime = *out.LastModified
	} else {
		fmt.Printf("no modtime for %s\n", f.name)
	}

	f.fi = &fi

	return &fi, nil
}

func (f *s3File) ReadDir(n int) ([]fs.DirEntry, error) {
	// s3 directories are just prefixes that end in /
	key := path.Join(f.root, f.name) + "/"

	if n < 0 {
		n = 0
	}

	params := s3.ListObjectsV2Input{
		Delimiter:         aws.String("/"),
		Bucket:            &f.bucket,
		Prefix:            &key,
		MaxKeys:           int32(10),
		ContinuationToken: f.contToken,
	}

	out, err := f.client.ListObjectsV2(f.ctx, &params)
	if err != nil {
		return nil, err
	}

	// EOF must be returned at the end of the directory when n > 0
	if out.KeyCount == 0 && n > 0 {
		return nil, io.EOF
	}

	f.contToken = out.ContinuationToken

	dirents := make([]fs.DirEntry, out.KeyCount)

	// these are files
	for i, obj := range out.Contents {
		dirent := staticFileInfo{
			size: obj.Size,
			mode: 0o644,
		}

		if obj.LastModified != nil {
			dirent.modTime = *obj.LastModified
		}

		if obj.Key != nil {
			dirent.name = *obj.Key
		}

		dirents[i] = &dirent
	}

	// these are directories
	for i, dir := range out.CommonPrefixes {
		dirent := s3DirEntry{
			mode:   fs.ModeDir,
			client: f.client,
			bucket: f.bucket,
		}

		if dir.Prefix != nil {
			dirent.key = *dir.Prefix
		}

		dirents[i+len(out.Contents)] = &dirent
	}

	return dirents, nil
}

type s3DirEntry struct {
	ctx    context.Context
	client *s3.Client
	bucket string
	key    string
	mode   fs.FileMode
}

var _ fs.DirEntry = (*s3DirEntry)(nil)

func (de *s3DirEntry) Name() string {
	return path.Base(de.key)
}

func (de *s3DirEntry) Info() (fs.FileInfo, error) {
	name := path.Base(de.key)
	root := path.Dir(de.key)

	f := &s3File{
		ctx:    de.ctx,
		bucket: de.bucket,
		name:   name,
		root:   root,
		client: de.client,
	}

	return f.Stat()
}

func (de *s3DirEntry) Type() fs.FileMode {
	return de.mode.Type()
}

func (de *s3DirEntry) Mode() fs.FileMode {
	return de.mode
}

func (de *s3DirEntry) IsDir() bool {
	return de.mode.IsDir()
}
