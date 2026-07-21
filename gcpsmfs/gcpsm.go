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
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
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

// secretCache holds fetched secret payloads and version metadata, keyed by
// "project/name". Two separate sync.Maps allow loadContent and ensureModTime
// to operate concurrently without contention.
//
// The cache is unbounded and entries are never invalidated or expired for
// the lifetime of the FS instance. Long-running processes that need to
// observe secret rotations, or that access a very large number of distinct
// secrets, should disable it via WithCacheFS(false, fsys) or by setting the
// GCP_SM_DISABLE_CACHE environment variable (which controls the default used
// by New).
type secretCache struct {
	content sync.Map // key: "project/name" → []byte
	modTime sync.Map // key: "project/name" → time.Time
}

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

// withMaxConcurrencyer is an fs.FS that can be configured with a concurrency limit.
type withMaxConcurrencyer interface {
	WithMaxConcurrency(n int) fs.FS
}

// WithMaxConcurrencyFS sets the maximum number of secrets fetched concurrently
// during a directory listing, if the filesystem supports it. Values <= 0 are
// ignored. The default is controlled by the GCP_SM_MAX_CONCURRENCY environment
// variable, falling back to 1 (serial) if unset.
func WithMaxConcurrencyFS(n int, fsys fs.FS) fs.FS {
	if fsys, ok := fsys.(withMaxConcurrencyer); ok {
		return fsys.WithMaxConcurrency(n)
	}

	return fsys
}

// withCacheEnabler is an fs.FS that can be configured to enable or disable
// its in-memory secret cache.
type withCacheEnabler interface {
	WithCache(enabled bool) fs.FS
}

// WithCacheFS enables or disables the in-memory secret cache used by fs, if
// the filesystem supports it. The cache stores fetched secret payloads and
// version metadata for the lifetime of the FS instance and never expires or
// invalidates entries (see secretCache), so disable it if the process is
// long-running and needs to observe secret rotations, or accesses a very
// large number of distinct secrets. The default is controlled by the
// GCP_SM_DISABLE_CACHE environment variable, falling back to enabled if unset
// or invalid.
func WithCacheFS(enabled bool, fsys fs.FS) fs.FS {
	if fsys, ok := fsys.(withCacheEnabler); ok {
		return fsys.WithCache(enabled)
	}

	return fsys
}

type gcpsmFS struct {
	ctx            context.Context
	smclient       SecretManagerClient
	base           *url.URL
	httpclient     *http.Client
	project        string
	maxConcurrency int
	cache          *secretCache
}

// New provides a filesystem (an fs.FS) backed by the GCP Secret Manager,
// rooted at the given URL.
//
// The URL should use the "gcp+sm" scheme and one of the following formats:
//   - "gcp+sm:///projects/<project-id>" to set an explicit project context
//   - "gcp+sm:///" for no project context
//
// A context can be given by using WithContextFS.
// defaultMaxConcurrency reads GCP_SM_MAX_CONCURRENCY from the environment,
// returning 1 if unset or invalid.
func defaultMaxConcurrency() int {
	if s := os.Getenv("GCP_SM_MAX_CONCURRENCY"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return n
		}
	}

	return 1
}

// defaultCacheEnabled reads GCP_SM_DISABLE_CACHE from the environment; any
// truthy value (as parsed by strconv.ParseBool) disables the in-memory
// secret cache. Returns true (enabled) if unset or invalid.
func defaultCacheEnabled() bool {
	if s := os.Getenv("GCP_SM_DISABLE_CACHE"); s != "" {
		if disabled, err := strconv.ParseBool(s); err == nil {
			return !disabled
		}
	}

	return true
}

func New(u *url.URL) (fs.FS, error) {
	if u.Scheme != "gcp+sm" {
		return nil, fmt.Errorf("invalid URL scheme %q", u.Scheme)
	}

	f := &gcpsmFS{
		ctx:            context.Background(),
		base:           u,
		maxConcurrency: defaultMaxConcurrency(),
	}

	if defaultCacheEnabled() {
		f.cache = &secretCache{}
	}

	// Normalize the path and validate it matches one of the supported forms:
	//   - "/" for no project context
	//   - "/projects/<project-id>" (optionally with a trailing slash)
	cleanPath := path.Clean(u.Path)
	// path.Clean("") returns ".", so treat that as no project context as well.
	if cleanPath == "." || cleanPath == "/" {
		// No project context.
		return f, nil
	}

	if project, found := strings.CutPrefix(cleanPath, "/projects/"); found {
		// Reject paths with extra segments like "/projects/p/secrets/foo".
		if project == "" || strings.Contains(project, "/") {
			return nil, fmt.Errorf("invalid gcp+sm URL path %q: expected /projects/<project-id> or /", u.Path)
		}

		f.project = project

		return f, nil
	}

	return nil, fmt.Errorf("invalid gcp+sm URL path %q: expected /projects/<project-id> or /", u.Path)
}

// FS is used to register this filesystem with an fsimpl.FSMux
//
//nolint:gochecknoglobals
var FS = fsimpl.FSProviderFunc(New, "gcp+sm")

var (
	_ fs.FS                     = (*gcpsmFS)(nil)
	_ fs.ReadFileFS             = (*gcpsmFS)(nil)
	_ fs.ReadDirFS              = (*gcpsmFS)(nil)
	_ internal.WithContexter    = (*gcpsmFS)(nil)
	_ internal.WithHTTPClienter = (*gcpsmFS)(nil)
	_ withSMClienter            = (*gcpsmFS)(nil)
	_ withMaxConcurrencyer      = (*gcpsmFS)(nil)
	_ withCacheEnabler          = (*gcpsmFS)(nil)
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

func (f *gcpsmFS) WithMaxConcurrency(n int) fs.FS {
	if n <= 0 {
		return f
	}

	fsys := *f
	fsys.maxConcurrency = n

	return &fsys
}

func (f *gcpsmFS) WithCache(enabled bool) fs.FS {
	fsys := *f

	if enabled {
		if fsys.cache == nil {
			fsys.cache = &secretCache{}
		}
	} else {
		fsys.cache = nil
	}

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

// getProjectAndFileName parses the project and file name out of name, given
// the FS's configured project (which may be empty). On error, it returns a
// plain fs.ErrInvalid (not wrapped in a *fs.PathError) so that callers can
// wrap it with their own operation name (e.g. "open", "readdir").
func (f *gcpsmFS) getProjectAndFileName(name string) (string, string, error) {
	// First, assume that the project is in the FS definition, not the path name
	project := f.project
	fileName := name

	// If no project is given by the FS, it must be in the file name, and must be extracted
	if project == "" {
		parts := strings.Split(name, "/")

		// A bare "projects/<id>" path represents the project directory itself.
		if len(parts) == 2 && parts[0] == "projects" && parts[1] != "" {
			return parts[1], ".", nil
		}

		if len(parts) != 4 || parts[0] != "projects" || parts[2] != "secrets" {
			return "", "", fs.ErrInvalid
		}

		project = parts[1]
		if project == "" {
			return "", "", fs.ErrInvalid
		}

		fileName = strings.TrimPrefix(path.Base(parts[3]), ".")
	}

	if strings.Contains(fileName, "/") {
		return "", "", fs.ErrInvalid
	}

	return project, fileName, nil
}

func (f *gcpsmFS) Open(name string) (fs.File, error) {
	if !internal.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}

	client, err := f.getClient()
	if err != nil {
		return nil, err
	}

	project, fileName, err := f.getProjectAndFileName(name)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: err}
	}

	file := &gcpsmFile{
		ctx:            f.ctx,
		name:           fileName,
		project:        project,
		client:         client,
		maxConcurrency: f.maxConcurrency,
		cache:          f.cache,
	}

	if fileName == "." {
		return file, nil
	}

	return file, nil
}

// opReaddir is the fs.PathError.Op used for all ReadDir-related errors below.
const opReaddir = "readdir"

func (f *gcpsmFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if !internal.ValidPath(name) {
		return nil, &fs.PathError{Op: opReaddir, Path: name, Err: fs.ErrInvalid}
	}

	// Root listings resolve directly to the FS's configured project (which may
	// be empty). Route through getProjectAndFileName only for non-root paths,
	// so an unscoped FS still gets the descriptive "requires a project" error
	// below instead of the generic fs.ErrInvalid that getProjectAndFileName
	// would otherwise return for ".".
	project, fileName := f.project, name

	if name != "." {
		var err error

		project, fileName, err = f.getProjectAndFileName(name)
		if err != nil {
			return nil, &fs.PathError{Op: opReaddir, Path: name, Err: err}
		}
	}

	if fileName != "." {
		return nil, &fs.PathError{Op: opReaddir, Path: name, Err: fs.ErrNotExist}
	}

	client, err := f.getClient()
	if err != nil {
		return nil, err
	}

	if project == "" {
		return nil, errors.New("listing secrets requires a project in the URL (e.g. gcp+sm:///projects/<project-id>)")
	}

	dir := &gcpsmFile{
		ctx:            f.ctx,
		name:           name,
		project:        project,
		client:         client,
		maxConcurrency: f.maxConcurrency,
		cache:          f.cache,
	}

	entries, err := dir.ReadDir(-1)
	if err != nil {
		return nil, &fs.PathError{Op: opReaddir, Path: name, Err: err}
	}

	return entries, nil
}

func (f *gcpsmFS) ReadFile(name string) ([]byte, error) {
	file, err := f.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return io.ReadAll(file)
}

// errIsDirectory is a copy of EISDIR for our purposes
var errIsDirectory = errors.New("is a directory")

type gcpsmFile struct {
	ctx            context.Context
	name           string
	project        string
	client         SecretManagerClient
	maxConcurrency int
	cache          *secretCache

	// modTime is set by ensureModTime after GetSecretVersion; nil means not loaded yet.
	modTime *time.Time
	body    io.Reader
	content []byte

	children []gcpsmFile
	diroff   int
}

var _ fs.ReadDirFile = (*gcpsmFile)(nil)

func (f *gcpsmFile) Close() error {
	return nil
}

func (f *gcpsmFile) Read(p []byte) (int, error) {
	if f.name == "." {
		return 0, fmt.Errorf("%w: %s", errIsDirectory, f.name)
	}

	if err := f.loadContent(); err != nil {
		return 0, &fs.PathError{Op: "read", Path: f.name, Err: err}
	}

	return f.body.Read(p)
}

func (f *gcpsmFile) getResourceName() string {
	return fmt.Sprintf("projects/%s/secrets/%s/versions/latest", f.project, f.name)
}

// fileInfo returns metadata for this handle. Directories (name ".") need no fetch;
// secret files must have loaded content and modTime (see loadContent / ensureModTime).
func (f *gcpsmFile) fileInfo() fs.FileInfo {
	if f.name == "." {
		return internal.DirInfo(f.name, time.Time{})
	}

	mt := time.Time{}
	if f.modTime != nil {
		mt = *f.modTime
	}

	return internal.FileInfo(f.name, int64(len(f.content)), 0o444, mt, "")
}

func (f *gcpsmFile) Stat() (fs.FileInfo, error) {
	if f.name == "." {
		return f.fileInfo(), nil
	}

	var g errgroup.Group
	g.Go(f.loadContent)
	g.Go(f.ensureModTime)

	if err := g.Wait(); err != nil {
		return nil, &fs.PathError{Op: "stat", Path: f.name, Err: err}
	}

	return f.fileInfo(), nil
}

// loadContent fetches the secret payload via AccessSecretVersion (one RPC when not cached).
func (f *gcpsmFile) loadContent() error {
	if f.content != nil {
		return nil
	}

	key := f.project + "/" + f.name

	if f.cache != nil {
		if v, ok := f.cache.content.Load(key); ok {
			payload := v.([]byte)
			f.content = payload
			f.body = bytes.NewReader(payload)

			return nil
		}
	}

	resourceName := f.getResourceName()

	req := &secretmanagerpb.AccessSecretVersionRequest{
		Name: resourceName,
	}

	resp, err := f.client.AccessSecretVersion(f.ctx, req)
	if err != nil {
		return convertGCPError(err)
	}

	var payload []byte
	if resp.Payload != nil {
		payload = resp.Payload.Data
	}

	if payload == nil {
		payload = []byte{}
	}

	f.content = payload
	f.body = bytes.NewReader(f.content)

	if f.cache != nil {
		f.cache.content.Store(key, f.content)
	}

	return nil
}

// ensureModTime loads version metadata via GetSecretVersion (one RPC when not cached).
func (f *gcpsmFile) ensureModTime() error {
	if f.modTime != nil {
		return nil
	}

	key := f.project + "/" + f.name

	if f.cache != nil {
		if v, ok := f.cache.modTime.Load(key); ok {
			t := v.(time.Time)
			f.modTime = &t

			return nil
		}
	}

	resourceName := f.getResourceName()

	getReq := &secretmanagerpb.GetSecretVersionRequest{
		Name: resourceName,
	}

	getResp, err := f.client.GetSecretVersion(f.ctx, getReq)
	if err != nil {
		return convertGCPError(err)
	}

	t := time.Time{}
	if getResp != nil && getResp.GetCreateTime() != nil {
		t = getResp.GetCreateTime().AsTime()
	}

	f.modTime = &t

	if f.cache != nil {
		f.cache.modTime.Store(key, t)
	}

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
		entries[i-low] = internal.FileInfoDirEntry(f.children[i].fileInfo())
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

	// Phase 1: drain the iterator serially — GCP iterators are not goroutine-safe.
	var secretNames []string

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
		secretNames = append(secretNames, parts[len(parts)-1])
	}

	// Phase 2: fetch content and mod-time for each secret concurrently, bounded
	// by maxConcurrency; values <= 0 default to 1 (serial).
	var (
		mu      sync.Mutex
		entries []gcpsmFile
	)

	limit := f.maxConcurrency
	if limit <= 0 {
		limit = 1
	}

	g, ctx := errgroup.WithContext(f.ctx)
	g.SetLimit(limit)

	for _, name := range secretNames {
		g.Go(func() error {
			child := gcpsmFile{
				name:    name,
				project: f.project,
				client:  f.client,
				cache:   f.cache,
			}

			// child is a disposable, per-goroutine value (never reused for
			// further RPCs once list() returns), so it's safe to rebind its
			// ctx to a derived, per-secret context: if either RPC fails, the
			// other is cancelled early instead of waiting out its full RPC.
			inner, innerCtx := errgroup.WithContext(ctx)
			child.ctx = innerCtx

			inner.Go(child.loadContent)
			inner.Go(child.ensureModTime)

			if err := inner.Wait(); err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return nil
				}

				return fmt.Errorf("while fetching secret %s: %w", name, err)
			}

			mu.Lock()

			entries = append(entries, child)
			mu.Unlock()

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return err
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
	case codes.FailedPrecondition:
		// A version in DISABLED or DESTROYED state triggers this code.
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
