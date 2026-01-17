package consulfs

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/hashicorp/consul/api"
)

// fakeConsulServer creates a fake Consul server with a predefined set of keys
// for testing common scenarios. It includes both root-level keys and keys under
// the /dir prefix to support different test scenarios.
func fakeConsulServer(t *testing.T) *api.Config {
	t.Helper()

	files := map[string]consulKVEntry{
		"/v1/kv/":                   {Keys: []string{"bar", "dir/", "foo"}},
		"/v1/kv/foo":                {Value: "foo value"},
		"/v1/kv/bar":                {Value: "bar value"},
		"/v1/kv/dir/":               {Keys: []string{"dir/bar", "dir/foo", "dir/sub/"}},
		"/v1/kv/dir/foo":            {Value: "foo"},
		"/v1/kv/dir/bar":            {Value: "foo"},
		"/v1/kv/dir/sub/":           {Keys: []string{"dir/sub/bar", "dir/sub/bazDir/", "dir/sub/foo"}},
		"/v1/kv/dir/sub/foo":        {Value: "foo"},
		"/v1/kv/dir/sub/bar":        {Value: "foo"},
		"/v1/kv/dir/sub/bazDir/":    {Keys: []string{"dir/sub/bazDir/qux"}},
		"/v1/kv/dir/sub/bazDir/qux": {Value: "qux"},
	}

	srv := httptest.NewServer(fakeConsulHandler(t, files))
	t.Cleanup(srv.Close)

	return &api.Config{Address: srv.URL}
}

// consulKVEntry represents a key-value entry in the fake Consul server
type consulKVEntry struct {
	Value string   `json:"value,omitempty"`
	Keys  []string `json:"keys,omitempty"`
}

// fakeConsulHandler creates an HTTP handler for a fake Consul KV API
// that responds based on the provided files map
func fakeConsulHandler(t *testing.T, files map[string]consulKVEntry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t.Helper()

		t.Logf("req to path %+v", r.URL.Path)

		data, ok := files[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)

			return
		}

		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)

		q := r.URL.Query()
		if q.Has("keys") {
			t.Logf("returning keys %+v", data.Keys)

			_ = enc.Encode(data.Keys)

			return
		}

		var pairs []*api.KVPair

		if !q.Has("recurse") {
			key := r.URL.Path[len("/v1/kv/"):]
			pairs = handleGetRequest(t, w, key, data)

			if pairs == nil {
				return
			}
		} else {
			for k, v := range files {
				if k == r.URL.Path {
					continue
				}

				if strings.HasPrefix(k, r.URL.Path) {
					pairs = append(pairs, &api.KVPair{
						Key:   k[len("/v1/kv/"):],
						Value: []byte(v.Value),
					})
				}
			}

			sort.Slice(pairs, func(i, j int) bool {
				return pairs[i].Key < pairs[j].Key
			})
		}

		t.Logf("returning pairs %+v", pairs)

		_ = enc.Encode(pairs)
	}
}

// handleGetRequest handles a single GET request to the fake Consul KV API
func handleGetRequest(t *testing.T, w http.ResponseWriter, key string, data consulKVEntry) []*api.KVPair {
	t.Helper()

	// Real Consul rejects GET requests with empty key names
	if key == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Missing key name"))

		return nil
	}

	// directories (paths ending with /) don't exist as keys in Consul
	if data.Value == "" {
		w.WriteHeader(http.StatusNotFound)

		return nil
	}

	return []*api.KVPair{{
		Key:   key,
		Value: []byte(data.Value),
	}}
}
