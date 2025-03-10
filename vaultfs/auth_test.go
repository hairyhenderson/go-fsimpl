package vaultfs

import (
	"encoding/json"
	"net/http"
	"os"
	"path"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/hairyhenderson/go-fsimpl/internal/tests/fakevault"
	"github.com/hashicorp/vault/api"
	"github.com/stretchr/testify/assert"
)

func TestEnvAuthLogin(t *testing.T) {
	v := fakevault.Server(t)

	ctx := t.Context()

	t.Setenv("VAULT_TOKEN", "foo")

	m := EnvAuthMethod()
	err := m.Login(ctx, v)
	assert.NoError(t, err)
	assert.Equal(t, "foo", v.Token())
	assert.NotNil(t, m.(*envAuthMethod).chosen)

	err = m.Logout(ctx, v)
	assert.NoError(t, err)
	assert.Equal(t, "", v.Token())
	assert.Nil(t, m.(*envAuthMethod).chosen)
}

func TestTokenLogin(t *testing.T) {
	ctx := t.Context()

	client := &api.Client{}

	// use env var if none provided
	t.Setenv("VAULT_TOKEN", "foo")

	m := TokenAuthMethod("")
	err := m.Login(ctx, client)
	assert.NoError(t, err)
	assert.Equal(t, "foo", client.Token())

	// use provided token, ignore env var
	m = TokenAuthMethod("bar")
	err = m.Login(ctx, client)
	assert.NoError(t, err)
	assert.Equal(t, "bar", client.Token())

	// support VAULT_TOKEN_FILE
	os.Unsetenv("VAULT_TOKEN")

	t.Setenv("VAULT_TOKEN_FILE", "/tmp/file")

	fsys := fstest.MapFS{}
	fsys["tmp/file"] = &fstest.MapFile{Data: []byte("tempfiletoken")}

	m = &tokenAuthMethod{fsys: fsys}
	err = m.Login(ctx, client)
	assert.NoError(t, err)
	assert.Equal(t, "tempfiletoken", client.Token())

	// fall back to ~/.vault-token
	os.Unsetenv("VAULT_TOKEN_FILE")

	homedir, _ := os.UserHomeDir()
	p := path.Join(homedir, ".vault-token")
	p = strings.TrimPrefix(p, "/")
	fsys[p] = &fstest.MapFile{Data: []byte("filetoken")}

	m = &tokenAuthMethod{fsys: fsys}
	err = m.Login(ctx, client)
	assert.NoError(t, err)
	assert.Equal(t, "filetoken", client.Token())
}

func TestAppRoleAuthMethod(t *testing.T) {
	mount := "approle"
	token := "approletoken"

	client := fakevault.FakeVault(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/auth/"+mount+"/login", r.URL.Path)

		out := map[string]any{
			"auth": map[string]any{
				"client_token": token,
			},
		}

		enc := json.NewEncoder(w)
		_ = enc.Encode(out)
	}))

	ctx := t.Context()

	m := AppRoleAuthMethod("", "", "")
	err := m.Login(ctx, client)
	assert.Error(t, err)

	m = AppRoleAuthMethod("some_id", "", "")
	err = m.Login(ctx, client)
	assert.Error(t, err)

	m = AppRoleAuthMethod("r", "s", "")
	err = m.Login(ctx, client)
	assert.NoError(t, err)
	assert.Equal(t, token, client.Token())

	mount = "elorppa"
	m = AppRoleAuthMethod("r", "s", mount)
	err = m.Login(ctx, client)
	assert.NoError(t, err)
	assert.Equal(t, token, client.Token())
}

//nolint:funlen
func TestUserPassAuthMethod(t *testing.T) {
	token := "sometoken"
	username := "alice"

	loginHandler := func(path string) http.HandlerFunc {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, path+"login/"+username, r.URL.Path)

			out := map[string]any{
				"auth": map[string]any{
					"client_token": token,
				},
			}

			enc := json.NewEncoder(w)
			_ = enc.Encode(out)
		})
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/auth/userpass/", loginHandler("/v1/auth/userpass/"))
	mux.HandleFunc("/v1/auth/ssapresu/", loginHandler("/v1/auth/ssapresu/"))
	mux.HandleFunc("/v1/auth/token/", func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/auth/token/revoke-self", r.URL.Path)

		out := map[string]any{
			"auth": map[string]any{
				"client_token": token,
			},
		}

		enc := json.NewEncoder(w)
		_ = enc.Encode(out)
	})

	client := fakevault.FakeVault(t, mux)

	ctx := t.Context()

	m := UserPassAuthMethod("", "", "")
	err := m.Login(ctx, client)
	assert.Error(t, err)

	m = UserPassAuthMethod("some_id", "", "")
	err = m.Login(ctx, client)
	assert.Error(t, err)

	m = UserPassAuthMethod(username, "s", "")
	err = m.Login(ctx, client)
	assert.NoError(t, err)
	assert.Equal(t, token, client.Token())

	err = m.Logout(ctx, client)
	assert.NoError(t, err)
	assert.Equal(t, "", client.Token())

	m = UserPassAuthMethod(username, "s", "ssapresu")
	err = m.Login(ctx, client)
	assert.NoError(t, err)
	assert.Equal(t, token, client.Token())

	err = m.Logout(ctx, client)
	assert.NoError(t, err)
	assert.Equal(t, "", client.Token())
}

func TestGitHubAuthMethod(t *testing.T) {
	mount := "github"
	token := "sometoken"
	ghtoken := "abcd1234"

	client := fakevault.FakeVault(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/auth/"+mount+"/login", r.URL.Path)

		out := map[string]any{
			"auth": map[string]any{
				"client_token": token,
			},
		}

		enc := json.NewEncoder(w)
		_ = enc.Encode(out)
	}))

	ctx := t.Context()

	m := GitHubAuthMethod("", "")
	err := m.Login(ctx, client)
	assert.Error(t, err)

	m = GitHubAuthMethod(ghtoken, "")
	err = m.Login(ctx, client)
	assert.NoError(t, err)
	assert.Equal(t, token, client.Token())

	mount = "buhtig"
	m = GitHubAuthMethod(ghtoken, mount)
	err = m.Login(ctx, client)
	assert.NoError(t, err)
	assert.Equal(t, token, client.Token())
}
