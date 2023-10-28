package vaultfs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync/atomic"
	"time"

	"github.com/hairyhenderson/go-fsimpl"
	"github.com/hairyhenderson/go-fsimpl/internal"
	"github.com/hairyhenderson/go-fsimpl/vaultfs/vaultauth"
	"github.com/hashicorp/vault/api"
)

type vaultFS struct {
	ctx  context.Context
	base *url.URL
	auth api.AuthMethod

	client *refCountedClient
}

// New creates a filesystem for the Vault endpoint rooted at u.
//
// It is especially important to make sure that opened files are closed,
// otherwise a Vault token may be leaked!
//
// The filesystem may be configured with:
//
//   - [vaultauth.WithAuthMethod] (set the auth method)
//   - [fsimpl.WithContextFS] (inject a context)
//   - [fsimpl.WithHeaderFS] (inject custom HTTP headers)
func New(u *url.URL) (fs.FS, error) {
	if u == nil {
		return nil, fmt.Errorf("url must not be nil")
	}

	if u.Path == "" {
		u.Path = "/"
	}

	if !strings.HasSuffix(u.Path, "/") {
		return nil, fmt.Errorf("invalid url path %q must be a prefix ending with \"/\"", u)
	}

	config, err := vaultConfig(u)
	if err != nil {
		return nil, fmt.Errorf("vault configuration error: %w", err)
	}

	c, err := api.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("vault client creation failed: %w", err)
	}

	fsys := newWithVaultClient(u, newRefCountedClient(c))
	fsys.auth = vaultauth.NewTokenAuth("")

	return fsys, nil
}

func newWithVaultClient(u *url.URL, client *refCountedClient) *vaultFS {
	base := *u
	if base.Path == "" || base.Path == "/" {
		base.Path = "/v1/"
	} else if !strings.HasPrefix(base.Path, "/v1") {
		base.Path = path.Join("/v1", path.Dir(base.Path)) + "/"
	}

	return &vaultFS{
		ctx:    context.Background(),
		client: client,
		base:   &base,
	}
}

func vaultConfig(u *url.URL) (*api.Config, error) {
	config := api.DefaultConfig()
	if config.Error != nil {
		return nil, config.Error
	}

	// handle compound URL scheme not supported by the client, but only if the
	// URL has a host part set - otherwise use the scheme from $VAULT_ADDR, as
	// set by api.DefaultConfig() above
	if u.Host != "" {
		scheme := strings.TrimPrefix(u.Scheme, "vault+")
		if scheme == "vault" {
			scheme = "https"
		}

		config.Address = scheme + "://" + u.Host
	}

	return config, nil
}

// FS is used to register this filesystem with an fsimpl.FSMux
//
//nolint:gochecknoglobals
var FS = fsimpl.FSProviderFunc(New, "vault", "vault+http", "vault+https")

var (
	_ fs.FS                  = (*vaultFS)(nil)
	_ fs.ReadFileFS          = (*vaultFS)(nil)
	_ internal.WithContexter = (*vaultFS)(nil)
	_ internal.WithHeaderer  = (*vaultFS)(nil)
)

func (f vaultFS) WithContext(ctx context.Context) fs.FS {
	fsys := f
	fsys.ctx = ctx

	return &fsys
}

func (f vaultFS) WithHeader(headers http.Header) fs.FS {
	fsys := f

	for k, vs := range headers {
		for _, v := range vs {
			fsys.client.AddHeader(k, v)
		}
	}

	return &fsys
}

func (f vaultFS) WithAuthMethod(auth api.AuthMethod) fs.FS {
	fsys := f
	fsys.auth = auth

	return &fsys
}

func (f vaultFS) Open(name string) (fs.File, error) {
	if !internal.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}

	u, err := f.subURL(name)
	if err != nil {
		return nil, err
	}

	if f.auth == nil {
		return nil, fmt.Errorf("missing vault auth method: %q", f.client.Token())
	}

	return newVaultFile(f.ctx, name, u, f.client, f.auth), nil
}

// ReadFile implements fs.ReadFileFS
//
// This implementation minimises Vault token uses by avoiding an extra Stat.
func (f vaultFS) ReadFile(name string) ([]byte, error) {
	if !internal.ValidPath(name) {
		return nil, &fs.PathError{Op: "readFile", Path: name, Err: fs.ErrInvalid}
	}

	opened, err := f.Open(name)
	if err != nil {
		return nil, err
	}
	defer opened.Close()

	b, err := io.ReadAll(opened)
	if err != nil {
		return nil, err
	}

	return b, nil
}

func (f *vaultFS) subURL(name string) (*url.URL, error) {
	rel, err := url.Parse(name)
	if err != nil {
		return nil, err
	}

	return f.base.ResolveReference(rel), nil
}

// newVaultFile opens a vault file/dir for reading - if this file is not closed
// a vault token may be leaked!
func newVaultFile(ctx context.Context, name string, u *url.URL, client *refCountedClient, auth api.AuthMethod) *vaultFile {
	// add reference to shared client - will be removed on Close
	client.AddRef()

	return &vaultFile{
		ctx:    ctx,
		name:   name,
		u:      u,
		client: client,
		auth:   auth,
	}
}

type vaultFile struct {
	ctx    context.Context
	name   string
	u      *url.URL
	client *refCountedClient
	auth   api.AuthMethod

	body     io.ReadCloser
	children []string
	diridx   int

	closed int32
}

var _ fs.ReadDirFile = (*vaultFile)(nil)

func (f *vaultFile) newRequest(method string) (*api.Request, error) {
	q := f.u.Query()
	if len(q) > 0 && method == http.MethodGet {
		method = http.MethodPost
	}

	req := f.client.NewRequest(method, f.u.Path)
	if method == http.MethodGet {
		req.Params = q
	} else if len(q) > 0 {
		data := map[string]interface{}{}
		for k, vs := range q {
			for _, v := range vs {
				data[k] = v
			}
		}

		err := req.SetJSONBody(data)
		if err != nil {
			return nil, err
		}
	}

	return req, nil
}

func (f *vaultFile) newKVv2Request(method string) *api.Request {
	// path gets munged for KV v2 - "data/" is added after the mountpoint for
	// reads, "metadata/" for lists
	mount, rest, _ := strings.Cut(strings.TrimPrefix(f.u.Path, "/v1/"), "/")

	p := path.Join("/v1", mount)
	if method == http.MethodGet {
		p = path.Join(p, "data", rest)
	} else if method == "LIST" {
		p = path.Join(p, "metadata", rest)
	}

	req := f.client.NewRequest(method, p)

	q := f.u.Query()
	for k := range q {
		req.Params.Set(k, q.Get(k))
	}

	return req
}

func (f *vaultFile) request(method string) (resp *api.Response, isV2 bool, err error) {
	if f.client.Token() == "" {
		var secret *api.Secret

		secret, err = f.auth.Login(f.ctx, f.client.Client)
		if err != nil {
			return nil, false, fmt.Errorf("vault login failure: %w", err)
		}

		f.client.SetToken(secret.Auth.ClientToken)
	}

	var req *api.Request

	isV2 = f.isKVv2()
	if isV2 {
		req = f.newKVv2Request(method)
	} else {
		req, err = f.newRequest(method)
		if err != nil {
			return nil, false, fmt.Errorf("failed to create vault request: %w", err)
		}
	}

	//nolint:staticcheck
	resp, err = f.client.RawRequestWithContext(f.ctx, req)
	if err != nil {
		return nil, isV2, fmt.Errorf("http %s %s failed with: %w", method, f.u.Path,
			vaultFSError(err))
	}

	if resp.StatusCode == 0 || resp.StatusCode >= 400 {
		return nil, isV2, fmt.Errorf("http %s %s failed with status %d", method, f.u, resp.StatusCode)
	}

	return resp, isV2, nil
}

// Close the file. Will error on second call. Decrements the ref count on first
// call and logs out of vault when the ref count reaches zero.
func (f *vaultFile) Close() error {
	// important to know the state of the file so that we don't
	if atomic.LoadInt32(&f.closed) == 1 {
		return &fs.PathError{Op: "close", Path: f.name, Err: fs.ErrClosed}
	}

	// mark closed
	atomic.StoreInt32(&f.closed, 1)

	f.client.RemoveRef()

	if f.client.Refs() == 0 {
		// the token auth method manages its own logout, to avoid revoking the
		// token, which shouldn't be managed here
		if lauth, ok := f.auth.(authLogouter); ok {
			lauth.Logout(f.client.Client)
		} else {
			revokeToken(f.ctx, f.client.Client)
		}
	}

	if f.body == nil {
		return nil
	}

	return f.body.Close()
}

func (f *vaultFile) Read(p []byte) (int, error) {
	if f.body != nil {
		return f.body.Read(p)
	}

	resp, isV2, err := f.request(http.MethodGet)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	s, err := api.ParseSecret(resp.Body)
	if err != nil {
		return 0, &fs.PathError{
			Op: "read", Path: f.name,
			Err: fmt.Errorf("failed to parse vault secret: %w", err),
		}
	}

	var b []byte

	if s != nil && s.Data != nil {
		if isV2 {
			b, err = json.Marshal(s.Data["data"])
		} else {
			b, err = json.Marshal(s.Data)
		}

		if err != nil {
			return 0, &fs.PathError{
				Op: "read", Path: f.name,
				Err: fmt.Errorf("unexpected failure to marshal vault secret: %w", err),
			}
		}
	}

	f.body = io.NopCloser(bytes.NewReader(b))

	return f.body.Read(p)
}

//nolint:funlen
func (f *vaultFile) Stat() (fs.FileInfo, error) {
	resp, isV2, err := f.request(http.MethodGet)

	rerr := &api.ResponseError{}
	if errors.As(err, &rerr) {
		// if it's a 404 it might be a directory - let's try to LIST it instead
		if rerr.StatusCode != http.StatusNotFound {
			return nil, &fs.PathError{
				Op: "stat", Path: f.name,
				Err: vaultFSError(err),
			}
		}
	} else if err != nil {
		_, err = f.list()
		if err != nil {
			return nil, &fs.PathError{Op: "stat", Path: f.name, Err: err}
		}

		fi := internal.DirInfo(strings.TrimSuffix(path.Base(f.name), "/"), time.Time{})

		return fi, nil
	}

	defer resp.Body.Close()

	secret, err := api.ParseSecret(resp.Body)
	if err != nil {
		return nil, &fs.PathError{
			Op: "stat", Path: f.name,
			Err: fmt.Errorf("failed to parse vault secret: %w", err),
		}
	}

	if secret == nil || secret.Data == nil {
		return nil, &fs.PathError{
			Op: "stat", Path: f.name, Err: fmt.Errorf("malformed secret"),
		}
	}

	var (
		b       []byte
		modTime time.Time
	)

	if isV2 {
		b, err = json.Marshal(secret.Data["data"])
		modTime = createdTimeFromData(secret.Data)
	} else {
		b, err = json.Marshal(secret.Data)
	}

	if err != nil {
		return nil, &fs.PathError{
			Op: "stat", Path: f.name,
			Err: fmt.Errorf("malformed secret: %w", err),
		}
	}

	return internal.FileInfo(
		strings.TrimSuffix(path.Base(f.name), "/"),
		int64(len(b)),
		0o644,
		modTime,
		"application/json",
	), nil
}

func (f *vaultFile) list() ([]string, error) {
	resp, _, err := f.request("LIST")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	s, err := api.ParseSecret(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse vault response: %w", err)
	}

	keys, ok := s.Data["keys"]
	if !ok {
		return nil, fmt.Errorf("keys missing from vault LIST response")
	}

	k, ok := keys.([]interface{})
	if !ok {
		return nil, fmt.Errorf("keys returned in unexpected format from vault LIST response: %#v", keys)
	}

	dirkeys := make([]string, len(k))

	for i := 0; i < len(k); i++ {
		if s, ok := k[i].(string); ok {
			dirkeys[i] = s
		}
	}

	return dirkeys, nil
}

func (f *vaultFile) childFile(childName string) *vaultFile {
	parent := *f.u
	parent.Path += "/"

	u, _ := url.Parse(childName)
	childURL := (&parent).ResolveReference(u)

	return newVaultFile(f.ctx, childName, childURL, f.client, f.auth)
}

func (f *vaultFile) ReadDir(n int) ([]fs.DirEntry, error) {
	// first call lists everything and caches the entries
	if f.children == nil {
		entries, err := f.list()
		if err != nil {
			return nil, &fs.PathError{Op: "readDir", Path: f.name, Err: err}
		}

		f.children = entries
	}

	dirents := []fs.DirEntry{}

	for i := f.diridx; n <= 0 || (n > 0 && i < n+f.diridx); i++ {
		// if we don't have enough children left
		if i >= len(f.children) {
			if n <= 0 {
				break
			}

			return dirents, io.EOF
		}

		childName := f.children[i]

		// vault lists directories with trailing slashes
		if strings.HasSuffix(childName, "/") {
			fi := internal.DirInfo(childName[:len(childName)-1], time.Time{})
			dirents = append(dirents, internal.FileInfoDirEntry(fi))

			continue
		}

		child := f.childFile(childName)
		defer child.Close()

		fi, err := child.Stat()
		if err != nil {
			return nil, &fs.PathError{Op: "readDir", Path: f.name, Err: err}
		}

		dirents = append(dirents, internal.FileInfoDirEntry(fi))
	}

	f.diridx += len(dirents)

	return dirents, nil
}

// vaultFSError converts from a vault API error to an appropriate filesystem
// error, preventing Vault API types from leaking
func vaultFSError(err error) error {
	rerr := &api.ResponseError{}
	if errors.As(err, &rerr) {
		errDetails := strings.Join(rerr.Errors, ", ")
		if errDetails != "" {
			errDetails = ", details: " + errDetails
		}

		return fmt.Errorf("%s %s - %d%s: %w",
			rerr.HTTPMethod,
			rerr.URL,
			rerr.StatusCode,
			errDetails,
			fs.ErrNotExist,
		)
	}

	return err
}

// isKVv2 - figure out if our path is on a KV v2 engine. Errors just result in
// false being returned - we'll get an error when we try to read the file later!
//
// To consider: cache the result for each mount point - this isn't necessarily
// straightforward as KV v1 mount points can be upgraded to v2 at runtime...
func (f *vaultFile) isKVv2() bool {
	nonV1Path := strings.TrimPrefix(f.u.Path, "/v1/")
	p := path.Join("/v1/sys/internal/ui/mounts", nonV1Path)

	r := f.client.NewRequest(http.MethodGet, p)

	//nolint:staticcheck
	resp, err := f.client.RawRequestWithContext(f.ctx, r)
	// defer the close in all cases - when errors are returned, the response
	// body may not be nil
	if resp != nil {
		defer resp.Body.Close()
	}

	if err != nil {
		return false
	}

	secret, err := api.ParseSecret(resp.Body)
	if err != nil || secret == nil {
		return false
	}

	// v2 response has an "options" key, assume v1 if it's missing
	options, ok := secret.Data["options"].(map[string]interface{})
	if !ok {
		return false
	}

	// v2 response has a "version" option, assume v1 if it's missing
	version, ok := options["version"].(string)
	if !ok {
		return false
	}

	return version == "2"
}

func createdTimeFromData(data map[string]interface{}) time.Time {
	t := time.Time{}

	metadata, ok := data["metadata"].(map[string]interface{})
	if !ok {
		return t
	}

	createdTime, ok := metadata["created_time"].(string)
	if !ok {
		return t
	}

	created, err := time.Parse(time.RFC3339Nano, createdTime)
	if err != nil {
		return t
	}

	return created
}
