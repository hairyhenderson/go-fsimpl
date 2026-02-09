package gcpsmfs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"github.com/hairyhenderson/go-fsimpl"
	"github.com/hairyhenderson/go-fsimpl/internal"
	"golang.org/x/sync/errgroup"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// withSMClienter is an fs.FS that can be configured to use the given Secrets
// Manager client.
type withSMClienter interface {
	WithSMClient(smclient SecretManagerClient) fs.FS
}

// WithSMClientFS overrides the GCP Secrets Manager client used by fs, if the
// filesystem supports it (i.e. has a WithSMClient method). This can be used for
// configuring specialized client options.
//
// Note that this should not be used together with WithHTTPClient. If you wish
// only to override the HTTP client, use WithHTTPClient alone.
func WithSMClientFS(smclient SecretManagerClient, fsys fs.FS) fs.FS {
	if fsys, ok := fsys.(withSMClienter); ok {
		return fsys.WithSMClient(smclient)
	}

	return fsys
}

type gcpsmFS struct {
	ctx        context.Context
	smclient   SecretManagerClient
	base       *url.URL
	httpclient *http.Client
	project    string
}

// New provides a filesystem (an fs.FS) backed by the GCP Secret Manager,
// rooted at the given URL.
//
// The URL should use the "gcp+sm" scheme and one of the following formats:
//   - "gcp+sm:///projects/<project-id>" to set an explicit project context
//   - "gcp+sm:///" for no project context
//
// A context can be given by using WithContextFS.
func New(u *url.URL) (fs.FS, error) {
	if u.Scheme != "gcp+sm" {
		return nil, fmt.Errorf("invalid URL scheme %q", u.Scheme)
	}

	f := &gcpsmFS{
		ctx:  context.Background(),
		base: u,
	}

	if strings.HasPrefix(u.Path, "/projects/") {
		f.project = strings.TrimPrefix(u.Path, "/projects/")
	}

	return f, nil
}

// FS is used to register this filesystem with an fsimpl.FSMux
//
//nolint:gochecknoglobals
var FS = fsimpl.FSProviderFunc(New, "gcp+sm")

var (
	_ fs.FS                     = (*gcpsmFS)(nil)
	_ fs.ReadFileFS             = (*gcpsmFS)(nil)
	_ fs.ReadDirFS              = (*gcpsmFS)(nil)
	_ fs.SubFS                  = (*gcpsmFS)(nil)
	_ internal.WithContexter    = (*gcpsmFS)(nil)
	_ internal.WithHTTPClienter = (*gcpsmFS)(nil)
	_ withSMClienter            = (*gcpsmFS)(nil)
)

func (f gcpsmFS) URL() string {
	return f.base.String()
}

func (f *gcpsmFS) WithContext(ctx context.Context) fs.FS {
	if ctx == nil {
		return f
	}

	fsys := *f
	fsys.ctx = ctx

	return &fsys
}

func (f *gcpsmFS) WithHTTPClient(client *http.Client) fs.FS {
	if client == nil {
		return f
	}

	fsys := *f
	fsys.httpclient = client

	return &fsys
}

func (f *gcpsmFS) WithSMClient(smclient SecretManagerClient) fs.FS {
	if smclient == nil {
		return f
	}

	fsys := *f
	fsys.smclient = smclient

	return &fsys
}

func (f *gcpsmFS) getClient() (SecretManagerClient, error) {
	if f.smclient != nil {
		return f.smclient, nil
	}

	opts := []option.ClientOption{}
	if f.httpclient != nil {
		opts = append(opts, option.WithHTTPClient(f.httpclient))
	}

	c, err := secretmanager.NewClient(f.ctx, opts...)
	if err != nil {
		return nil, err
	}

	f.smclient = &clientAdapter{c}

	return f.smclient, nil
}

func (f *gcpsmFS) Sub(name string) (fs.FS, error) {
	// Since we are flat, Sub implies we can't go deeper unless name is "."
	if !internal.ValidPath(name) {
		return nil, &fs.PathError{Op: "sub", Path: name, Err: fs.ErrInvalid}
	}

	if name == "." || name == "" {
		return f, nil
	}

	return nil, &fs.PathError{Op: "sub", Path: name, Err: errors.New("subdirectories not supported in gcpsmfs")}
}

func (f *gcpsmFS) Open(name string) (fs.File, error) {
	if !internal.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}

	client, err := f.getClient()
	if err != nil {
		return nil, err
	}

	project := f.project
	fileName := name

	if project == "" {
		parts := strings.Split(name, "/")
		if len(parts) != 4 || parts[0] != "projects" || parts[2] != "secrets" {
			return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
		}

		project = parts[1]
		if project == "" {
			return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
		}

		fileName = strings.TrimPrefix(path.Base(parts[3]), ".")
	}

	file := &gcpsmFile{
		ctx:     f.ctx,
		name:    fileName,
		project: project,
		client:  client,
	}

	if fileName == "." {
		file.fi = internal.DirInfo(file.name, time.Time{})

		return file, nil
	}

	if strings.Contains(fileName, "/") {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}

	return file, nil
}

func (f *gcpsmFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if !internal.ValidPath(name) {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrInvalid}
	}

	if name != "." {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: errors.New("not a directory")}
	}

	client, err := f.getClient()
	if err != nil {
		return nil, err
	}

	project := f.project

	if project == "" {
		parts := strings.Split(name, "/")
		if len(parts) != 4 {
			return nil, errors.New("expected file in the form projects/<project>/secrets/<secret>")
		}

		project = parts[1]
		if project == "" {
			return nil, errors.New("project ID is required in URL (e.g. gcp+sm://projects/<project>/secrets/<secret>)")
		}
	}

	dir := &gcpsmFile{
		ctx:     f.ctx,
		name:    name,
		project: f.project,
		client:  client,
		fi:      internal.DirInfo(name, time.Time{}),
	}

	return dir.ReadDir(-1)
}

func (f *gcpsmFS) ReadFile(name string) ([]byte, error) {
	if !internal.ValidPath(name) {
		return nil, &fs.PathError{Op: "readFile", Path: name, Err: fs.ErrInvalid}
	}

	client, err := f.getClient()
	if err != nil {
		return nil, err
	}

	resourceName := name + "/versions/latest"
	if f.project != "" {
		resourceName = fmt.Sprintf("projects/%s/secrets/%s/versions/latest", f.project, name)
	}

	req := &secretmanagerpb.AccessSecretVersionRequest{
		Name: resourceName,
	}

	resp, err := client.AccessSecretVersion(f.ctx, req)
	if err != nil {
		return nil, &fs.PathError{Op: "readFile", Path: name, Err: convertGCPError(err)}
	}

	return resp.Payload.Data, nil
}

type gcpsmFile struct {
	ctx     context.Context
	name    string
	project string
	client  SecretManagerClient

	fi   fs.FileInfo
	body io.Reader

	children []gcpsmFile
	diroff   int
}

var _ fs.ReadDirFile = (*gcpsmFile)(nil)

func (f *gcpsmFile) Close() error {
	return nil
}

func (f *gcpsmFile) Read(p []byte) (int, error) {
	if f.body == nil {
		if err := f.fetch(); err != nil {
			return 0, &fs.PathError{Op: "read", Path: f.name, Err: err}
		}
	}

	return f.body.Read(p)
}

func (f *gcpsmFile) Stat() (fs.FileInfo, error) {
	if f.fi != nil {
		return f.fi, nil
	}

	if err := f.fetch(); err != nil {
		// If fetch fails, we might still be a directory (if we were opened as "." but logic is tricky here)
		// But Open(".") sets fi, so we are good.
		return nil, &fs.PathError{Op: "stat", Path: f.name, Err: err}
	}

	return f.fi, nil
}

func (f *gcpsmFile) fetch() error {
	resourceName := fmt.Sprintf("projects/%s/secrets/%s/versions/latest", f.project, f.name)

	var (
		getResp *secretmanagerpb.SecretVersion
		resp    *secretmanagerpb.AccessSecretVersionResponse
	)

	g, ctx := errgroup.WithContext(f.ctx)

	g.Go(func() error {
		var err error

		getReq := &secretmanagerpb.GetSecretVersionRequest{
			Name: resourceName,
		}
		getResp, err = f.client.GetSecretVersion(ctx, getReq)

		return convertGCPError(err)
	})

	g.Go(func() error {
		var err error

		req := &secretmanagerpb.AccessSecretVersionRequest{
			Name: resourceName,
		}
		resp, err = f.client.AccessSecretVersion(ctx, req)

		return convertGCPError(err)
	})

	if err := g.Wait(); err != nil {
		return err
	}

	modTime := time.Time{}
	if getResp != nil && getResp.GetCreateTime() != nil {
		modTime = getResp.GetCreateTime().AsTime()
	}

	f.body = bytes.NewReader(resp.Payload.Data)
	size := int64(len(resp.Payload.Data))

	f.fi = internal.FileInfo(f.name, size, 0o444, modTime, "")

	return nil
}

func (f *gcpsmFile) ReadDir(n int) ([]fs.DirEntry, error) {
	if f.children == nil {
		if err := f.list(); err != nil {
			return nil, fmt.Errorf("list: %w", err)
		}
	}

	if n > 0 && f.diroff >= len(f.children) {
		return nil, io.EOF
	}

	low := f.diroff
	high := f.diroff + n

	// clamp high at the max, and ensure it's higher than low
	if high >= len(f.children) || high <= low {
		high = len(f.children)
	}

	entries := make([]fs.DirEntry, high-low)
	for i := low; i < high; i++ {
		entries[i-low] = internal.FileInfoDirEntry(f.children[i].fi)
	}

	f.diroff = high

	return entries, nil
}

func (f *gcpsmFile) list() error {
	parent := "projects/" + f.project
	req := &secretmanagerpb.ListSecretsRequest{
		Parent: parent,
	}

	it := f.client.ListSecrets(f.ctx, req)

	var entries []gcpsmFile

	for {
		secret, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}

		if err != nil {
			return convertGCPError(err)
		}

		// Name is full resource name: projects/{project}/secrets/{name}
		parts := strings.Split(secret.Name, "/")
		name := parts[len(parts)-1]

		child := gcpsmFile{
			ctx:     f.ctx,
			name:    name,
			project: f.project,
			client:  f.client,
		}
		// needed to get file info
		err = child.fetch()
		if err != nil {
			return fmt.Errorf("while fetching secret %s: %w", name, err)
		}

		entries = append(entries, child)
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	f.children = entries

	return nil
}

func convertGCPError(err error) error {
	if err == nil {
		return nil
	}

	st, ok := status.FromError(err)
	if !ok {
		return err
	}

	switch st.Code() { //nolint:exhaustive
	case codes.NotFound:
		return fmt.Errorf("%w: %s", fs.ErrNotExist, st.Message())
	case codes.PermissionDenied:
		return fmt.Errorf("%w: %s", fs.ErrPermission, st.Message())
	case codes.Unauthenticated:
		return fmt.Errorf("%w: %s", fs.ErrPermission, st.Message())
	case codes.InvalidArgument:
		return fmt.Errorf("%w: %s", fs.ErrInvalid, st.Message())
	default:
		return err
	}
}
