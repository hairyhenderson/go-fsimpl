package vaultauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/hashicorp/vault/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fakeVault(t *testing.T, handler http.Handler) *api.Client {
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	tr := &http.Transport{
		Proxy: func(req *http.Request) (*url.URL, error) {
			return url.Parse(srv.URL)
		},
	}
	httpClient := &http.Client{Transport: tr}
	config := &api.Config{Address: srv.URL, HttpClient: httpClient}

	c, _ := api.NewClient(config)

	return c
}

func TestGitHubAuthMethod(t *testing.T) {
	mount := "github"
	token := "sometoken"
	ghtoken := "abcd1234"

	client := fakeVault(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/auth/"+mount+"/login", r.URL.Path)

		out := map[string]interface{}{
			"auth": map[string]interface{}{
				"client_token": token,
			},
		}

		enc := json.NewEncoder(w)
		_ = enc.Encode(out)
	}))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, err := NewGitHubAuth(nil)
	require.Error(t, err)
	_, err = NewGitHubAuth(&GitHubToken{})
	require.Error(t, err)
	_, err = NewGitHubAuth(&GitHubToken{FromFile: "foo", FromEnv: "bar"})
	require.Error(t, err)
	_, err = NewGitHubAuth(&GitHubToken{FromFile: "foo", FromString: "bar"})
	require.Error(t, err)
	_, err = NewGitHubAuth(&GitHubToken{FromEnv: "foo", FromString: "bar"})
	require.Error(t, err)
	_, err = NewGitHubAuth(&GitHubToken{FromFile: "foo", FromEnv: "foo", FromString: "bar"})
	require.Error(t, err)

	a, err := NewGitHubAuth(&GitHubToken{FromString: ghtoken})
	require.NoError(t, err)
	s, err := a.Login(ctx, client)
	require.NoError(t, err)
	assert.Equal(t, token, s.Auth.ClientToken)

	mount = "buhtig"
	a, err = NewGitHubAuth(&GitHubToken{FromString: ghtoken}, WithGitHubMountPath(mount))
	require.NoError(t, err)
	s, err = a.Login(ctx, client)
	require.NoError(t, err)
	assert.Equal(t, token, s.Auth.ClientToken)
}
