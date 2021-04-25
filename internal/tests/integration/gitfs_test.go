package integration

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/hairyhenderson/go-fsimpl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tfs "gotest.tools/v3/fs"
	"gotest.tools/v3/icmd"
)

func setupDatasourcesGitTest(t *testing.T) *tfs.Dir {
	tmpDir := tfs.NewDir(t, "gomplate-inttests",
		tfs.WithDir("repo",
			tfs.WithFiles(map[string]string{
				"config.json": `{"foo": {"bar": "baz"}}`,
			}),
			tfs.WithDir("dir",
				tfs.WithFiles(map[string]string{
					"file1.txt": "hello",
					"file2.txt": "world",
				}),
				tfs.WithDir("subdir",
					tfs.WithFiles(map[string]string{
						"sub.yaml": "foo:\n\tbar: true",
					}),
				),
			),
		),
	)
	t.Cleanup(tmpDir.Remove)

	repoPath := tmpDir.Join("repo")

	result := icmd.RunCommand("git", "init", repoPath)
	result.Assert(t, icmd.Expected{ExitCode: 0, Out: "Initialized empty Git repository"})

	result = icmd.RunCmd(icmd.Command("git", "add", "-A"), icmd.Dir(repoPath))
	result.Assert(t, icmd.Expected{ExitCode: 0})

	result = icmd.RunCmd(icmd.Command("git", "commit", "-m", "Initial commit"), icmd.Dir(repoPath))
	result.Assert(t, icmd.Expected{ExitCode: 0})

	return tmpDir
}

func startGitDaemon(t *testing.T) string {
	tmpDir := setupDatasourcesGitTest(t)

	pidDir := tfs.NewDir(t, "gomplate-inttests-pid")
	t.Cleanup(pidDir.Remove)

	port, addr := freeport()
	gitDaemon := icmd.Command("git", "daemon",
		"--verbose",
		"--port="+strconv.Itoa(port),
		"--base-path="+tmpDir.Path(),
		"--pid-file="+pidDir.Join("git.pid"),
		"--export-all",
		tmpDir.Join("repo", ".git"),
	)
	gitDaemon.Stdin = nil
	gitDaemon.Stdout = &bytes.Buffer{}
	gitDaemon.Dir = tmpDir.Path()
	result := icmd.StartCmd(gitDaemon)

	t.Cleanup(func() {
		err := result.Cmd.Process.Kill()
		require.NoError(t, err)

		_, err = result.Cmd.Process.Wait()
		require.NoError(t, err)

		result.Assert(t, icmd.Expected{ExitCode: 0})
	})

	// give git time to start
	time.Sleep(500 * time.Millisecond)

	return addr
}

func TestGitFS_File(t *testing.T) {
	tmpDir := setupDatasourcesGitTest(t)

	repoPath := filepath.ToSlash(tmpDir.Join("repo"))
	u, _ := url.Parse("git+file://" + repoPath)

	fsys := fsimpl.GitFS(u)
	f, err := fsys.Open("config.json")
	assert.NoError(t, err)

	b, err := io.ReadAll(f)
	assert.NoError(t, err)
	assert.Equal(t, `{"foo": {"bar": "baz"}}`, string(b))

	u, _ = url.Parse("git+file://" + repoPath + "//dir")
	fsys = fsimpl.GitFS(u)
	_, err = fsys.Open("config.json")
	assert.Error(t, err)

	files, err := fs.ReadDir(fsys, ".")
	assert.NoError(t, err)
	assert.Len(t, files, 3)

	assert.Equal(t, "file1.txt", files[0].Name())
	assert.Equal(t, "file2.txt", files[1].Name())
	assert.Equal(t, "subdir", files[2].Name())

	subdir := files[2]
	assert.True(t, subdir.IsDir())
}

func TestGitFS_Daemon(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("not running on Windows")
	}

	addr := startGitDaemon(t)

	u, _ := url.Parse("git://" + addr + "/repo//dir")
	fsys := fsimpl.GitFS(u)

	files, err := fs.ReadDir(fsys, ".")
	assert.NoError(t, err)
	assert.Len(t, files, 3)

	assert.Equal(t, "file1.txt", files[0].Name())
	assert.Equal(t, "file2.txt", files[1].Name())
	assert.Equal(t, "subdir", files[2].Name())

	subdir := files[2]
	assert.True(t, subdir.IsDir())
}

func TestGitFS_HTTPDatasource(t *testing.T) {
	u, _ := url.Parse("git+https://github.com/git-fixtures/basic//json/")
	fsys := fsimpl.GitFS(u)

	files, err := fs.ReadDir(fsys, ".")
	assert.NoError(t, err)
	assert.Len(t, files, 2)

	fi, err := fs.Stat(fsys, "short.json")
	assert.NoError(t, err)
	assert.Equal(t, int64(706), fi.Size())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	fsys = fsimpl.GitFS(u)
	fsys = fsimpl.WithContextFS(ctx, fsys)

	_, err = fs.ReadDir(fsys, ".")
	assert.ErrorIs(t, err, context.Canceled)
}

func TestGitFS_SSHDatasource(t *testing.T) {
	if os.Getenv("SSH_AUTH_SOCK") == "" {
		t.Skip("SSH Agent not running")
	}

	u, _ := url.Parse("git+ssh://git@github.com/git-fixtures/basic//json")
	fsys := fsimpl.GitFS(u)

	files, err := fs.ReadDir(fsys, ".")
	assert.NoError(t, err)
	assert.Len(t, files, 2)
}
