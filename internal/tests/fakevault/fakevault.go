package fakevault

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/hashicorp/vault/api"
	"github.com/stretchr/testify/assert"
)

func mountHandler(w http.ResponseWriter, _ *http.Request) {
	mounts := map[string]any{
		"secret/": map[string]any{
			"type": "kv",
		},
	}

	resp := map[string]any{
		"data": map[string]any{
			"secret": mounts,
		},
	}

	enc := json.NewEncoder(w)
	_ = enc.Encode(resp)
}

//nolint:gocyclo
func vaultHandler(t *testing.T, files map[string]fakeSecret) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "LIST" || (r.Method == http.MethodGet && r.URL.Query().Get("list") == "true") {
			r.URL.Path += "/"

			// transform back to list for simplicity
			r.Method = "LIST"
			vals := r.URL.Query()
			vals.Del("list")
			r.URL.RawQuery = vals.Encode()
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

		body := map[string]any{}

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
		_ = enc.Encode(map[string]any{"data": data})
	})
}

type fakeSecret struct {
	Value string   `json:"value,omitempty"`
	Param string   `json:"param,omitempty"`
	Keys  []string `json:"keys,omitempty"`
}

func Server(t *testing.T) *api.Client {
	files := map[string]fakeSecret{
		"/v1/secret/":            {Keys: []string{"foo", "bar", "foo/"}},
		"/v1/secret/foo":         {Value: "foo"},
		"/v1/secret/bar":         {Value: "foo"},
		"/v1/secret/foo/":        {Keys: []string{"foo", "bar", "bazDir/"}},
		"/v1/secret/foo/foo":     {Value: "foo"},
		"/v1/secret/foo/bar":     {Value: "foo"},
		"/v1/secret/foo/bazDir/": {Keys: []string{"foo", "bar", "bazDir/"}},
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/v1/sys/internal/ui/mounts", mountHandler)
	mux.Handle("/", vaultHandler(t, files))

	return FakeVault(t, mux)
}

func FakeVault(t *testing.T, handler http.Handler) *api.Client {
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	tr := &http.Transport{
		Proxy: func(_ *http.Request) (*url.URL, error) {
			return url.Parse(srv.URL)
		},
	}
	httpClient := &http.Client{Transport: tr}
	config := &api.Config{Address: srv.URL, HttpClient: httpClient}

	c, _ := api.NewClient(config)

	return c
}
