package gitfs

import (
	"encoding/base64"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/user"
	"testing"
	"testing/fstest"

	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/hairyhenderson/go-fsimpl/internal/tests"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh/testdata"
)

func TestAutoAuthenticator(t *testing.T) {
	a := AutoAuthenticator()

	_, err := a.Authenticate(tests.MustURL("bogus:///bare.git"))
	assert.Error(t, err)

	// valid for all supported schemes
	am, err := a.Authenticate(tests.MustURL("git://"))
	assert.NoError(t, err)
	// no-op returns a nil AuthMethod
	assert.Nil(t, am)

	am, err = a.Authenticate(tests.MustURL("file:///bare.git"))
	assert.NoError(t, err)
	// no-op returns a nil AuthMethod
	assert.Nil(t, am)

	// basic auth
	am, err = a.Authenticate(tests.MustURL("https://user@example.com"))
	assert.NoError(t, err)
	assert.EqualValues(t,
		&githttp.BasicAuth{Username: "user", Password: ""}, am)

	t.Run("with ssh-agent", func(t *testing.T) {
		if os.Getenv("SSH_AUTH_SOCK") == "" {
			t.Skip("SSH_AUTH_SOCK not set")
		}

		// ssh-agent auth
		am, err = a.Authenticate(tests.MustURL("ssh://git@example.com"))
		assert.NoError(t, err)

		pkc, ok := am.(*ssh.PublicKeysCallback)
		assert.True(t, ok)
		assert.Equal(t, "git", pkc.User)
	})

	// ssh public key auth with env var
	key := base64.StdEncoding.EncodeToString(testdata.PEMBytes["rsa"])

	os.Setenv("GIT_SSH_KEY", key)
	defer os.Unsetenv("GIT_SSH_KEY")

	am, err = a.Authenticate(tests.MustURL("ssh://git@example.com"))
	assert.NoError(t, err)

	pk, ok := am.(*ssh.PublicKeys)
	assert.True(t, ok)
	assert.Equal(t, "git", pk.User)
}

func ExampleAutoAuthenticator() {
	u, _ := url.Parse("https://github.com/git-fixtures/basic//json/")

	fsys, _ := New(u)

	// AutoAuthenticator is the default, so this is redundant, but left here for
	// documentation purposes.
	fsys = WithAuthenticator(AutoAuthenticator(), fsys)

	// this will use authenticated access if set in the environment, or default
	// to unauthenticated access.
	fi, _ := fs.Stat(fsys, "short.json")
	fmt.Printf("file size: %d\n", fi.Size())
}

func TestNoopAuthenticator(t *testing.T) {
	a := NoopAuthenticator()

	// only valid for git/file/http/https schemes
	_, err := a.Authenticate(tests.MustURL("ssh://example.com/foo"))
	assert.Error(t, err)

	am, err := a.Authenticate(tests.MustURL("git://"))
	assert.NoError(t, err)
	assert.Nil(t, am)

	am, err = a.Authenticate(tests.MustURL("file:///bare.git"))
	assert.NoError(t, err)
	assert.Nil(t, am)

	am, err = a.Authenticate(tests.MustURL("https://example.com"))
	assert.NoError(t, err)
	assert.Nil(t, am)

	am, err = a.Authenticate(tests.MustURL("http://example.com"))
	assert.NoError(t, err)
	assert.Nil(t, am)
}

func TestBasicAuthenticator(t *testing.T) {
	envfsys := fstest.MapFS{}
	a := &basicAuthenticator{envfsys: envfsys}

	os.Unsetenv("GIT_HTTP_PASSWORD")

	// only valid for http[s] schemes
	_, err := a.Authenticate(tests.MustURL("file:///bare.git"))
	assert.Error(t, err)

	// user/pass are not required with basic auth - this results in a no-op -
	// useful for public repos
	am, err := a.Authenticate(tests.MustURL("http://example.com/foo"))
	assert.NoError(t, err)
	assert.Nil(t, am)

	// credentials from URL
	am, err = a.Authenticate(tests.MustURL("https://user:swordfish@example.com/foo"))
	assert.NoError(t, err)
	assert.EqualValues(t,
		&githttp.BasicAuth{Username: "user", Password: "swordfish"}, am)

	// credentials from env used when none are provided
	os.Setenv("GIT_HTTP_PASSWORD", "swordfish")
	defer os.Unsetenv("GIT_HTTP_PASSWORD")

	am, err = a.Authenticate(tests.MustURL("https://example.com/foo"))
	assert.NoError(t, err)
	assert.EqualValues(t,
		&githttp.BasicAuth{Username: "", Password: "swordfish"}, am)

	// provided credentials override env used when none are provided
	os.Setenv("GIT_HTTP_PASSWORD", "pufferfish")
	defer os.Unsetenv("GIT_HTTP_PASSWORD")

	a.username = "user"
	a.password = "swordfish"
	am, err = a.Authenticate(tests.MustURL("http://example.com/foo"))
	assert.NoError(t, err)
	assert.EqualValues(t,
		&githttp.BasicAuth{Username: "user", Password: "swordfish"}, am)

	// credentials from URL override provided & env credentials
	am, err = a.Authenticate(tests.MustURL("https://foo:bar@example.com/foo"))
	assert.NoError(t, err)
	assert.EqualValues(t,
		&githttp.BasicAuth{Username: "foo", Password: "bar"}, am)
}

// Using Basic Auth:
func ExampleBasicAuthenticator() {
	u, _ := url.Parse("https://mysite.com/myrepo.git")

	fsys, _ := New(u)
	fsys = WithAuthenticator(BasicAuthenticator("me", "mypassword"), fsys)

	// this will use authenticated access
	fi, _ := fs.Stat(fsys, "short.json")
	fmt.Printf("file size: %d\n", fi.Size())

	// or we can set user/password in the URL:
	u, _ = url.Parse("https://me:mypassword@mysite.com/myrepo.git")

	fsys, _ = New(u)
	// no need to provide user/pass to the authenticator, but if we did, the URL
	// values would override.
	fsys = WithAuthenticator(BasicAuthenticator("", ""), fsys)

	fi, _ = fs.Stat(fsys, "short.json")
	fmt.Printf("file size: %d\n", fi.Size())
}

// Using Basic Auth with a public (unauthenticated) repo:
func ExampleBasicAuthenticator_unauthenticated() {
	u, _ := url.Parse("https://github.com/git-fixtures/basic//json/")

	fsys, _ := New(u)

	// Use BasicAuthenticator with no user/pass to get unauthenticated access.
	// See also NoopAuthenticator to prevent authentication from URL.
	fsys = WithAuthenticator(BasicAuthenticator("", ""), fsys)

	fi, _ := fs.Stat(fsys, "short.json")
	fmt.Printf("file size: %d\n", fi.Size())
}

func TestTokenAuthenticator(t *testing.T) {
	envfsys := fstest.MapFS{}
	a := &tokenAuthenticator{envfsys: envfsys}

	os.Unsetenv("GIT_HTTP_TOKEN")

	// only valid for http[s] schemes
	_, err := a.Authenticate(tests.MustURL("file:///bare.git"))
	assert.Error(t, err)

	// token must be set _somewhere_
	_, err = a.Authenticate(tests.MustURL("https://example.com/foo"))
	assert.Error(t, err)

	// token from env
	os.Setenv("GIT_HTTP_TOKEN", "foo")
	defer os.Unsetenv("GIT_HTTP_TOKEN")

	am, err := a.Authenticate(tests.MustURL("http://example.com/foo"))
	assert.NoError(t, err)
	assert.EqualValues(t, &githttp.TokenAuth{Token: "foo"}, am)

	// provided token overrides env
	a.token = "bar"
	am, err = a.Authenticate(tests.MustURL("https://example.com/foo"))
	assert.NoError(t, err)
	assert.EqualValues(t, &githttp.TokenAuth{Token: "bar"}, am)
}

//nolint:funlen
func TestPublicKeyAuthenticator(t *testing.T) {
	envfsys := fstest.MapFS{}
	a := &publicKeyAuthenticator{envfsys: envfsys}

	os.Unsetenv("GIT_SSH_KEY")

	// only valid for ssh schemes
	_, err := a.Authenticate(tests.MustURL("file:///bare.git"))
	assert.Error(t, err)

	_, err = a.Authenticate(tests.MustURL("https:///bare.git"))
	assert.Error(t, err)

	// key must be set _somewhere_
	_, err = a.Authenticate(tests.MustURL("ssh://example.com/foo"))
	assert.Error(t, err)

	// key from env, base64-encoded
	key := string(testdata.PEMBytes["ed25519"])

	enckey := base64.StdEncoding.EncodeToString([]byte(key))

	os.Setenv("GIT_SSH_KEY", enckey)
	defer os.Unsetenv("GIT_SSH_KEY")

	am, err := a.Authenticate(tests.MustURL("ssh://example.com/foo"))
	assert.NoError(t, err)
	assert.IsType(t, &ssh.PublicKeys{}, am)

	// key from file referenced by env (non-base64)
	os.Unsetenv("GIT_SSH_KEY")

	os.Setenv("GIT_SSH_KEY_FILE", "/testdata/key.pem")
	defer os.Unsetenv("GIT_SSH_KEY_FILE")

	envfsys["testdata/key.pem"] = &fstest.MapFile{Data: []byte(key)}

	am, err = a.Authenticate(tests.MustURL("ssh://example.com/foo"))
	assert.NoError(t, err)
	assert.IsType(t, &ssh.PublicKeys{}, am)

	// provided key overrides env
	os.Unsetenv("GIT_SSH_KEY_FILE")

	os.Setenv("GIT_SSH_KEY", "unparseable key, but will be ignored")
	defer os.Unsetenv("GIT_SSH_KEY")

	a.username = "user"
	a.privKey = testdata.PEMBytes["ed25519"]
	am, err = a.Authenticate(tests.MustURL("ssh://example.com/foo"))
	assert.NoError(t, err)
	assert.IsType(t, &ssh.PublicKeys{}, am)

	pk := am.(*ssh.PublicKeys)
	assert.Equal(t, "user", pk.User)

	// username in URL overrides provided
	am, err = a.Authenticate(tests.MustURL("ssh://git@example.com/foo"))
	assert.NoError(t, err)
	assert.IsType(t, &ssh.PublicKeys{}, am)

	pk = am.(*ssh.PublicKeys)
	assert.Equal(t, "git", pk.User)
}

func TestSSHAgentAuthenticator(t *testing.T) {
	if os.Getenv("SSH_AUTH_SOCK") == "" {
		t.Skip("no SSH_AUTH_SOCK - skipping ssh agent test")
	}

	currentUser := ""
	if u, err := user.Current(); err == nil {
		currentUser = u.Username
	} else {
		currentUser = os.Getenv("USER")
	}

	require.NotEmpty(t, currentUser)

	a := &sshAgentAuthenticator{}

	// only valid for ssh schemes
	_, err := a.Authenticate(tests.MustURL("file:///bare.git"))
	assert.Error(t, err)

	_, err = a.Authenticate(tests.MustURL("https:///bare.git"))
	assert.Error(t, err)

	// user defaults to current user
	am, err := a.Authenticate(tests.MustURL("ssh://example.com/foo"))
	assert.NoError(t, err)

	pkc, ok := am.(*ssh.PublicKeysCallback)
	assert.Equal(t, true, ok)
	assert.Equal(t, currentUser, pkc.User)

	// provided user overrides
	a.username = "user"
	am, err = a.Authenticate(tests.MustURL("ssh://example.com/foo"))
	assert.NoError(t, err)

	pkc, ok = am.(*ssh.PublicKeysCallback)
	assert.Equal(t, true, ok)
	assert.Equal(t, "user", pkc.User)

	// username in URL overrides provided
	am, err = a.Authenticate(tests.MustURL("ssh://git@example.com"))
	assert.NoError(t, err)

	pkc, ok = am.(*ssh.PublicKeysCallback)
	assert.Equal(t, true, ok)
	assert.Equal(t, "git", pkc.User)
}
