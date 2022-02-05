package gitfs

import (
	"fmt"
	"io/fs"
	"net/url"
)

// Using gitfs.New to create a filesystem based on a git repository on the
// local filesystem.
func Example() {
	u, _ := url.Parse("file:///data/repo")
	fsys, _ := New(u)

	files, _ := fs.ReadDir(fsys, ".")
	for _, name := range files {
		fmt.Printf("file: %s\n", name.Name())
	}
}

// Using WithAuthenticator to configure authentication using the ssh-agent
// support.
func Example_explicitAuth() {
	u, _ := url.Parse("git+ssh://github.com/git-fixtures/basic//json#branch")

	// create the FS and set SSH Agent authentication explicitly, setting the
	// username to 'git', as GitHub requires.
	fsys, _ := New(u)
	fsys = WithAuthenticator(SSHAgentAuthenticator("git"), fsys)

	files, _ := fs.ReadDir(fsys, ".")
	for _, name := range files {
		fmt.Printf("file: %s\n", name.Name())
	}
}
