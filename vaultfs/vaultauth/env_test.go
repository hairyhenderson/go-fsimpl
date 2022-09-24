package vaultauth

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/hashicorp/vault/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnvAuthLogin(t *testing.T) {
	v := fakeVaultServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	os.Setenv("VAULT_TOKEN", "foo")
	defer os.Unsetenv("VAULT_TOKEN")

	m := EnvAuthMethod()
	s, err := m.Login(ctx, v)
	require.NoError(t, err)
	assert.Equal(t, "foo", s.Auth.ClientToken)
	assert.NotNil(t, m.(*envAuthMethod).chosen)
}

func fakeVaultServer(t *testing.T) *api.Client {
	files := map[string]struct {
		Value string   `json:"value,omitempty"`
		Param string   `json:"param,omitempty"`
		Keys  []string `json:"keys,omitempty"`
	}{
		"/v1/secret/":            {Keys: []string{"foo", "bar", "foo/"}},
		"/v1/secret/foo":         {Value: "foo"},
		"/v1/secret/bar":         {Value: "foo"},
		"/v1/secret/foo/":        {Keys: []string{"foo", "bar", "bazDir/"}},
		"/v1/secret/foo/foo":     {Value: "foo"},
		"/v1/secret/foo/bar":     {Value: "foo"},
		"/v1/secret/foo/bazDir/": {Keys: []string{"foo", "bar", "bazDir/"}},
	}

	return fakeVault(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "LIST" {
			r.URL.Path += "/"
		}

		data, ok := files[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)

			return
		}

		q := r.URL.Query()
		for k, v := range q {
			if k == "method" {
				assert.Equal(t, v[0], r.Method)
			}
		}
		body := map[string]interface{}{}
		if r.Body != nil {
			dec := json.NewDecoder(r.Body)
			_ = dec.Decode(&body)

			defer r.Body.Close()

			if p, ok := body["param"]; ok {
				data.Param = p.(string)
			}
		}

		switch r.Method {
		case http.MethodGet:
			assert.Empty(t, data.Param, r.URL)
			assert.NotEmpty(t, data.Value, r.URL)
		case http.MethodPost:
			assert.NotEmpty(t, data.Param, r.URL)
		case "LIST":
			assert.NotEmpty(t, data.Keys, r.URL)
		}

		t.Logf("encoding %#v", data)

		enc := json.NewEncoder(w)
		_ = enc.Encode(map[string]interface{}{"data": data})
	}))
}
