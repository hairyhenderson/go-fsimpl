package gitfs

import (
	"context"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/hairyhenderson/go-fsimpl"
	"github.com/hairyhenderson/go-fsimpl/internal"
	"github.com/hairyhenderson/go-fsimpl/internal/billyadapter"
)

type gitFS struct {
	ctx context.Context

	repofs  fs.FS
	envfsys fs.FS

	auth Authenticator

	repo *url.URL
	root string
}

// New provides a filesystem (an fs.FS) for the git repository indicated by
// the given URL. Valid schemes are "git", "file", "http", "https", "ssh", and
// the same prefixed with "git+" (e.g. "git+ssh://...")
//
// A context can be given by using WithContextFS.
func New(u *url.URL) (fs.FS, error) {
	repoURL := *u

	repoURL.Scheme = strings.TrimPrefix(repoURL.Scheme, "git+")

	repoPath, root := splitRepoPath(repoURL.Path)
	repoURL.Path = repoPath

	if root == "" {
		root = "/"
	}

	fsys := &gitFS{
		ctx:     context.Background(),
		repo:    &repoURL,
		root:    root,
		envfsys: os.DirFS("/"),
		auth:    AutoAuthenticator(),
	}

	return fsys, nil
}

// FS is used to register this filesystem with an fsimpl.FSMux
//
//nolint:gochecknoglobals
var FS = fsimpl.FSProviderFunc(New, "git", "git+file", "git+http", "git+https", "git+ssh")

var (
	_ fs.FS                  = (*gitFS)(nil)
	_ fs.ReadDirFS           = (*gitFS)(nil)
	_ internal.WithContexter = (*gitFS)(nil)
	_ withAuthenticatorer    = (*gitFS)(nil)
)

func (f gitFS) WithAuthenticator(auth Authenticator) fs.FS {
	fsys := f
	fsys.auth = auth

	return &fsys
}

func (f gitFS) WithContext(ctx context.Context) fs.FS {
	fsys := f
	fsys.ctx = ctx

	return &fsys
}

// validPath - return a valid path for fs.FS operations from a traditional path
func validPath(p string) string {
	if p == "/" || p == "" {
		return "."
	}

	return strings.TrimPrefix(p, "/")
}

func (f *gitFS) clone() (fs.FS, error) {
	if f.repofs == nil {
		depth := 1
		if f.repo.Scheme == "file" {
			// we can't do shallow clones for filesystem repos apparently
			depth = 0
		}

		bfs, _, err := f.gitClone(f.ctx, *f.repo, depth)
		if err != nil {
			return nil, err
		}

		fsys := billyadapter.BillyToFS(bfs)

		f.repofs, err = fs.Sub(fsys, validPath(f.root))
		if err != nil {
			return nil, err
		}
	}

	return f.repofs, nil
}

func (f *gitFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}

	fsys, err := f.clone()
	if err != nil {
		return nil, fmt.Errorf("open: failed to clone: %w", err)
	}

	return fsys.Open(name)
}

func (f *gitFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrInvalid}
	}

	fsys, err := f.clone()
	if err != nil {
		return nil, fmt.Errorf("readdir: failed to clone: %w", err)
	}

	return fs.ReadDir(fsys, name)
}

// Split the git repo path from the subpath, delimited by "//"
func splitRepoPath(repopath string) (repo, subpath string) {
	parts := strings.SplitN(repopath, "//", 2)
	switch len(parts) {
	case 1:
		subpath = "/"
	case 2:
		subpath = "/" + parts[1]

		i := strings.LastIndex(repopath, subpath)
		repopath = repopath[:i-1]
	}

	if subpath != "/" {
		subpath = strings.TrimSuffix(subpath, "/")
	}

	return repopath, subpath
}

func refFromURL(u url.URL) plumbing.ReferenceName {
	switch {
	case strings.HasPrefix(u.Fragment, "refs/"):
		return plumbing.ReferenceName(u.Fragment)
	case u.Fragment != "":
		return plumbing.NewBranchReferenceName(u.Fragment)
	default:
		return plumbing.ReferenceName("")
	}
}

// gitClone a repo for later reading through http(s), git, or ssh. u must be the URL to the repo
// itself, and must have any file path stripped
func (f *gitFS) gitClone(ctx context.Context, repoURL url.URL, depth int) (billy.Filesystem, *git.Repository, error) {
	// copy repoURL so we can perhaps use it later
	u := repoURL

	if f.auth == nil {
		return nil, nil, fmt.Errorf("clone: no auth method provided")
	}

	authMethod, err := f.auth.Authenticate(&u)
	if err != nil {
		return nil, nil, err
	}

	ref := refFromURL(u)
	u.Fragment = ""
	u.RawQuery = ""

	opts := git.CloneOptions{
		URL:           u.String(),
		Auth:          authMethod,
		Depth:         depth,
		ReferenceName: ref,
		SingleBranch:  true,
		Tags:          git.NoTags,
	}

	bfs := memfs.New()
	bfs = billyadapter.FrozenModTimeFilesystem(bfs, time.Now())

	storer := memory.NewStorage()

	repo, err := git.CloneContext(ctx, storer, bfs, &opts)

	if u.Scheme == "file" && err == transport.ErrRepositoryNotFound && !strings.HasSuffix(u.Path, ".git") {
		// maybe this has a `.git` subdirectory...
		u = repoURL
		u.Path = path.Join(u.Path, ".git")

		return f.gitClone(ctx, u, depth)
	}

	if err != nil {
		return nil, nil, fmt.Errorf("git clone for %v failed: %w", repoURL, err)
	}

	return bfs, repo, nil
}
