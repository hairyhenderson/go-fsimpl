package gitfs

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport/client"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/server"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/hairyhenderson/go-fsimpl"
	"github.com/hairyhenderson/go-fsimpl/internal/billyadapter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh/testdata"
)

func mustParseURL(in string) url.URL {
	u, _ := url.Parse(in)

	return *u
}

func TestSplitRepoPath(t *testing.T) {
	t.Parallel()

	u, _ := url.Parse("http://example.com//foo")
	assert.Equal(t, "//foo", u.Path)
	parts := strings.SplitN(u.Path, "//", 2)
	assert.Equal(t, 2, len(parts))
	assert.EqualValues(t, []string{"", "foo"}, parts)

	data := []struct {
		in         string
		repo, path string
	}{
		{"/hairyhenderson/gomplate//docs-src/content/functions/aws.yml", "/hairyhenderson/gomplate", "/docs-src/content/functions/aws.yml"},
		{"/hairyhenderson/gomplate.git", "/hairyhenderson/gomplate.git", "/"},
		{"/", "/", "/"},
		{"/foo//file.txt", "/foo", "/file.txt"},
		{"/home/foo/repo//file.txt", "/home/foo/repo", "/file.txt"},
		{"/repo", "/repo", "/"},
		{"/foo//foo", "/foo", "/foo"},
		{"/foo//foo/bar", "/foo", "/foo/bar"},
		{"/foo/bar", "/foo/bar", "/"},
		{"/foo//bar", "/foo", "/bar"},
		{"//foo/bar", "", "/foo/bar"},
		{"/foo//bar/baz", "/foo", "/bar/baz"},
		{"/foo/bar//baz", "/foo/bar", "/baz"},
		{"/foo/bar//baz/", "/foo/bar", "/baz"},
		{"//baz/", "", "/baz"},
		{"/foo//", "/foo", "/"},
	}

	for i, d := range data {
		d := d
		t.Run(fmt.Sprintf("%d:(%q)==(%q,%q)", i, d.in, d.repo, d.path), func(t *testing.T) {
			t.Parallel()

			repo, path := splitRepoPath(d.in)
			assert.Equal(t, d.repo, repo)
			assert.Equal(t, d.path, path)
		})
	}
}

//nolint:funlen
func setupGitRepo(t *testing.T) map[string]string {
	t.Helper()

	bfs := memfs.New()
	bfs = billyadapter.FrozenModTimeFilesystem(bfs, time.Now())

	err := bfs.MkdirAll("/repo/.git", os.ModeDir)
	require.NoError(t, err)

	repo, err := bfs.Chroot("/repo")
	require.NoError(t, err)
	dot, err := repo.Chroot("/.git")
	require.NoError(t, err)

	s := filesystem.NewStorage(dot, cache.NewObjectLRUDefault())

	r, err := git.Init(s, repo)
	require.NoError(t, err)

	// config needs to be created after setting up a "normal" fs repo
	// this is possibly a bug in src-d/git-go?
	c, err := r.Config()
	require.NoError(t, err)

	err = s.SetConfig(c)
	require.NoError(t, err)

	w, err := r.Worktree()
	require.NoError(t, err)

	_ = repo.MkdirAll("/foo/bar", os.ModeDir)
	f, err := repo.Create("/foo/bar/hi.txt")
	require.NoError(t, err)
	_, err = f.Write([]byte("hello world"))
	require.NoError(t, err)

	_, err = w.Add(f.Name())
	require.NoError(t, err)
	hash, err := w.Commit("initial commit", &git.CommitOptions{Author: &object.Signature{}})
	require.NoError(t, err)

	ref, err := r.CreateTag("v1", hash, nil)
	require.NoError(t, err)

	testHashes := map[string]string{}
	testHashes["v1"] = hash.String()

	branchName := plumbing.NewBranchReferenceName("mybranch")
	err = w.Checkout(&git.CheckoutOptions{
		Branch: branchName,
		Hash:   ref.Hash(),
		Create: true,
	})
	require.NoError(t, err)

	f, err = repo.Create("/secondfile.txt")
	require.NoError(t, err)
	n, err := f.Write([]byte("another file\n"))
	require.NoError(t, err)
	require.Equal(t, 13, n)

	_, err = w.Add(f.Name())
	require.NoError(t, err)

	hash, err = w.Commit("second commit", &git.CommitOptions{
		Author: &object.Signature{
			Name: "John Doe",
		},
	})
	require.NoError(t, err)

	ref = plumbing.NewHashReference(branchName, hash)

	testHashes["mybranch"] = ref.Hash().String()

	// make the repo dirty
	_, err = f.Write([]byte("dirty file"))
	require.NoError(t, err)

	// set up a bare repo
	_ = bfs.MkdirAll("/bare.git", os.ModeDir)
	_ = bfs.MkdirAll("/barewt", os.ModeDir)
	repo, _ = bfs.Chroot("/barewt")
	dot, _ = bfs.Chroot("/bare.git")
	s = filesystem.NewStorage(dot, nil)

	r, err = git.Init(s, repo)
	require.NoError(t, err)

	w, err = r.Worktree()
	require.NoError(t, err)

	f, err = repo.Create("/hello.txt")
	require.NoError(t, err)

	_, _ = f.Write([]byte("hello world"))

	_, _ = w.Add(f.Name())
	_, err = w.Commit("initial commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "John Doe",
			Email: "john@doe.org",
			When:  time.Now(),
		},
	})
	require.NoError(t, err)

	// override the go-git filesystem loader for file:// URLs
	client.InstallProtocol("file", server.NewClient(
		server.NewFilesystemLoader(bfs),
	))
	t.Cleanup(func() {
		client.InstallProtocol("file", server.DefaultServer)
	})

	return testHashes
}

func TestGitFS(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("not running on Windows yet...")
	}

	_ = setupGitRepo(t)

	u, _ := url.Parse("git+file:///repo")
	fsys, _ := New(u)

	require.NoError(t, fstest.TestFS(fsys, filepath.Join("foo", "bar", "hi.txt"), "secondfile.txt"))
}

func TestGitFS_Clone(t *testing.T) {
	ctx := context.Background()
	testHashes := setupGitRepo(t)

	fsys, _, err := gitClone(ctx, mustParseURL("file:///repo"), 0)
	assert.NoError(t, err)

	f, err := fsys.Open("/foo/bar/hi.txt")
	require.NoError(t, err)

	b, _ := io.ReadAll(f)
	assert.Equal(t, "hello world", string(b))

	_, repo, err := gitClone(ctx, mustParseURL("file:///repo#master"), 0)
	assert.NoError(t, err)

	ref, err := repo.Reference(plumbing.NewBranchReferenceName("master"), true)
	assert.NoError(t, err)
	assert.Equal(t, "refs/heads/master", ref.Name().String())

	_, repo, err = gitClone(ctx, mustParseURL("file:///repo#refs/tags/v1"), 0)
	assert.NoError(t, err)

	ref, err = repo.Head()
	assert.NoError(t, err)
	assert.Equal(t, testHashes["v1"], ref.Hash().String())

	_, repo, err = gitClone(ctx, mustParseURL("file:///repo/#mybranch"), 0)
	assert.NoError(t, err)

	ref, err = repo.Head()
	assert.NoError(t, err)
	assert.Equal(t, "refs/heads/mybranch", ref.Name().String())
	assert.Equal(t, testHashes["mybranch"], ref.Hash().String())
}

func TestGitFS_Clone_BareFileRepo(t *testing.T) {
	ctx := context.Background()
	_ = setupGitRepo(t)

	fsys, _, err := gitClone(ctx, mustParseURL("file:///bare.git"), 0)
	assert.NoError(t, err)

	f, err := fsys.Open("/hello.txt")
	require.NoError(t, err)

	b, _ := io.ReadAll(f)
	assert.Equal(t, "hello world", string(b))
}

func TestGitFS_ReadDir(t *testing.T) {
	_ = setupGitRepo(t)

	ctx := context.Background()

	u, _ := url.Parse("git+file:///bare.git")
	fsys, _ := New(u)
	fsys = fsimpl.WithContextFS(ctx, fsys)

	file, err := fsys.Open("hello.txt")
	assert.NoError(t, err)
	assert.NotNil(t, file)

	defer file.Close()

	fi, err := file.Stat()
	assert.NoError(t, err)
	assert.Equal(t, int64(11), fi.Size())

	b, _ := io.ReadAll(file)
	assert.Equal(t, "hello world", string(b))

	file, err = fsys.Open(".")
	assert.NoError(t, err)

	fi, err = file.Stat()
	assert.NoError(t, err)
	assert.True(t, fi.IsDir())

	dirents, err := file.(fs.ReadDirFile).ReadDir(-1)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(dirents))

	assert.Equal(t, "hello.txt", dirents[0].Name())
}

//nolint:funlen
func TestGitFS_Auth(t *testing.T) {
	a, err := auth(mustParseURL("file:///bare.git"))
	assert.NoError(t, err)
	assert.Equal(t, nil, a)

	a, err = auth(mustParseURL("https://example.com/foo"))
	assert.NoError(t, err)
	assert.Nil(t, a)

	a, err = auth(mustParseURL("https://user:swordfish@example.com/foo"))
	assert.NoError(t, err)
	assert.EqualValues(t, &http.BasicAuth{Username: "user", Password: "swordfish"}, a)

	os.Setenv("GIT_HTTP_PASSWORD", "swordfish")
	defer os.Unsetenv("GIT_HTTP_PASSWORD")

	a, err = auth(mustParseURL("https://user@example.com/foo"))
	assert.NoError(t, err)
	assert.EqualValues(t, &http.BasicAuth{Username: "user", Password: "swordfish"}, a)
	os.Unsetenv("GIT_HTTP_PASSWORD")

	os.Setenv("GIT_HTTP_TOKEN", "mytoken")
	defer os.Unsetenv("GIT_HTTP_TOKEN")

	a, err = auth(mustParseURL("https://user@example.com/foo"))
	assert.NoError(t, err)
	assert.EqualValues(t, &http.TokenAuth{Token: "mytoken"}, a)
	os.Unsetenv("GIT_HTTP_TOKEN")

	if os.Getenv("SSH_AUTH_SOCK") == "" {
		t.Log("no SSH_AUTH_SOCK - skipping ssh agent test")
	} else {
		a, err = auth(mustParseURL("ssh://git@example.com/foo"))
		assert.NoError(t, err)
		sa, ok := a.(*ssh.PublicKeysCallback)
		assert.Equal(t, true, ok)
		assert.Equal(t, "git", sa.User)
	}

	key := string(testdata.PEMBytes["ed25519"])

	os.Setenv("GIT_SSH_KEY", key)
	defer os.Unsetenv("GIT_SSH_KEY")

	a, err = auth(mustParseURL("ssh://git@example.com/foo"))
	assert.NoError(t, err)

	ka, ok := a.(*ssh.PublicKeys)
	assert.Equal(t, true, ok)
	assert.Equal(t, "git", ka.User)
	os.Unsetenv("GIT_SSH_KEY")

	key = base64.StdEncoding.EncodeToString(testdata.PEMBytes["ed25519"])

	os.Setenv("GIT_SSH_KEY", key)
	defer os.Unsetenv("GIT_SSH_KEY")

	a, err = auth(mustParseURL("ssh://git@example.com/foo"))
	assert.NoError(t, err)

	ka, ok = a.(*ssh.PublicKeys)
	assert.Equal(t, true, ok)
	assert.Equal(t, "git", ka.User)
	os.Unsetenv("GIT_SSH_KEY")
}

func TestGitFS_RefFromURL(t *testing.T) {
	t.Parallel()

	data := []struct {
		url, expected string
	}{
		{"git://localhost:1234/foo/bar.git//baz", ""},
		{"http://localhost:1234/foo/bar.git//baz", ""},
		{"ssh://localhost:1234/foo/bar.git//baz", ""},
		{"git+file:///foo/bar.git//baz", ""},
		{"git://localhost:1234/foo/bar.git//baz#master", "refs/heads/master"},
		{"git+http://localhost:1234/foo/bar.git//baz#mybranch", "refs/heads/mybranch"},
		{"git+ssh://localhost:1234/foo/bar.git//baz#refs/tags/foo", "refs/tags/foo"},
		{"git+file:///foo/bar.git//baz#mybranch", "refs/heads/mybranch"},
	}

	for _, d := range data {
		out := refFromURL(mustParseURL(d.url))
		assert.Equal(t, plumbing.ReferenceName(d.expected), out)
	}
}
