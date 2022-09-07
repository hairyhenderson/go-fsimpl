package vaultfs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/hairyhenderson/go-fsimpl"
	"github.com/hairyhenderson/go-fsimpl/internal"
	"github.com/hairyhenderson/go-fsimpl/internal/tests"
	"github.com/hashicorp/vault/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Example() {
	base, _ := url.Parse("vault://my.vaultserver.local:8200")
	token := "1234abcd"

	fsys, _ := New(base)
	fsys = WithAuthMethod(TokenAuthMethod(token), fsys)

	b, _ := fs.ReadFile(fsys, "secret/mysecret")

	// data returned by Vault is always JSON
	s := struct{ Value string }{}

	_ = json.Unmarshal(b, &s)

	fmt.Printf("the secret is %s\n", s.Value)
}

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

func TestVaultConfig(t *testing.T) {
	err := os.Unsetenv("VAULT_ADDR")
	assert.NoError(t, err)

	config, err := vaultConfig(tests.MustURL("vault:///"))
	assert.NoError(t, err)
	assert.Equal(t, "https://127.0.0.1:8200", config.Address)

	config, err = vaultConfig(tests.MustURL("vault://vault.example.com"))
	assert.NoError(t, err)
	assert.Equal(t, "https://vault.example.com", config.Address)
}

func TestNew(t *testing.T) {
	_, err := New(nil)
	assert.Error(t, err)

	_, err = New(tests.MustURL("vault:///secret/foo"))
	assert.Error(t, err)

	testdata := []struct {
		in, expected string
	}{
		{"vault:///", "vault:///v1/"},
		{"vault+https://example.com", "vault+https://example.com/v1/"},
		{"vault:///?param=value", "vault:///v1/?param=value"},
		{"vault:///secret/?param=value", "vault:///v1/secret/?param=value"},
		{"vault:///secret/?param=value", "vault:///v1/secret/?param=value"},
	}

	for _, d := range testdata {
		fsys, err := New(tests.MustURL(d.in))
		assert.NoError(t, err)
		require.IsType(t, &vaultFS{}, fsys)

		vfs := fsys.(*vaultFS)
		assert.Equal(t, d.expected, vfs.base.String())

		// defaults to env auth method
		assert.IsType(t, &envAuthMethod{}, vfs.auth)
	}
}

func TestWithContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), struct{}{}, "foo")

	fsys := &vaultFS{ctx: context.Background()}
	fsys = fsys.WithContext(ctx).(*vaultFS)

	assert.Same(t, ctx, fsys.ctx)
}

func TestWithHeader(t *testing.T) {
	fsys := &vaultFS{client: newRefCountedClient(fakeVaultServer(t))}

	fsys = fsys.WithHeader(http.Header{
		"foo": []string{"bar"},
	}).(*vaultFS)

	assert.Equal(t, "bar", fsys.client.Headers().Get("foo"))

	fsys = &vaultFS{client: newRefCountedClient(fakeVaultServer(t))}
	fsys.client.AddHeader("foo", "bar")

	fsys = fsys.WithHeader(http.Header{
		"foo": []string{"bar2"},
		"baz": []string{"qux"},
	}).(*vaultFS)

	assert.EqualValues(t, []string{"bar", "bar2"}, fsys.client.Headers().Values("foo"))
	assert.EqualValues(t, "qux", fsys.client.Headers().Get("baz"))
}

func TestOpen(t *testing.T) {
	fsys, err := New(tests.MustURL("vault+https://127.0.0.1:8200/secret/"))
	assert.NoError(t, err)

	_, err = fsys.Open("/bogus")
	assert.Error(t, err)

	if runtime.GOOS != "windows" {
		_, err = fsys.Open("bo\\gus")
		assert.Error(t, err)
	}
}

func jsonMap(b []byte) map[string]string {
	m := map[string]string{}
	_ = json.Unmarshal(b, &m)

	return m
}

func TestReadFile(t *testing.T) {
	expected := "{\"value\":\"foo\"}"
	v := newRefCountedClient(fakeVaultServer(t))

	fsys := fs.FS(newWithVaultClient(tests.MustURL("vault:///secret/"), v))
	fsys = WithAuthMethod(TokenAuthMethod("blargh"), fsys)

	f, err := fsys.Open("foo")
	assert.NoError(t, err)

	b, err := io.ReadAll(f)
	assert.NoError(t, err)
	assert.Equal(t, []byte(expected), b)

	b, err = fs.ReadFile(fsys, "bar")
	assert.NoError(t, err)
	assert.Equal(t, []byte(expected), b)

	b, err = fs.ReadFile(fsys, "foo?param=value&method=POST")
	assert.NoError(t, err)
	assert.EqualValues(t,
		map[string]string{"param": "value", "value": "foo"},
		jsonMap(b),
	)

	err = f.Close()
	assert.NoError(t, err)
}

func TestReadDirFS(t *testing.T) {
	v := newRefCountedClient(fakeVaultServer(t))

	fsys := fs.FS(newWithVaultClient(tests.MustURL("vault:///secret/foo/"), v))
	fsys = WithAuthMethod(TokenAuthMethod("blargh"), fsys)

	de, err := fs.ReadDir(fsys, ".")
	assert.NoError(t, err)

	des := []fs.DirEntry{
		internal.FileInfo("bar", 25, 0o644, time.Time{}, "application/json").(fs.DirEntry),
		internal.DirInfo("bazDir", time.Time{}).(fs.DirEntry),
		internal.FileInfo("foo", 25, 0o644, time.Time{}, "application/json").(fs.DirEntry),
	}
	assert.EqualValues(t, des, de)

	fsys = newWithVaultClient(tests.MustURL("vault:///secret/"), v)
	fsys = WithAuthMethod(TokenAuthMethod("blargh"), fsys)

	de, err = fs.ReadDir(fsys, "foo")
	assert.NoError(t, err)

	des = []fs.DirEntry{
		internal.FileInfo("bar", 25, 0o644, time.Time{}, "application/json").(fs.DirEntry),
		internal.DirInfo("bazDir", time.Time{}).(fs.DirEntry),
		internal.FileInfo("foo", 25, 0o644, time.Time{}, "application/json").(fs.DirEntry),
	}
	assert.EqualValues(t, des, de)
}

//nolint:funlen
func TestReadDirN(t *testing.T) {
	v := newRefCountedClient(fakeVaultServer(t))

	fsys := fs.FS(newWithVaultClient(tests.MustURL("vault:///secret/"), v))
	fsys = WithAuthMethod(TokenAuthMethod("blargh"), fsys)

	// open and read a few entries at a time
	df, err := fsys.Open("foo")
	assert.NoError(t, err)
	assert.Implements(t, (*fs.ReadDirFile)(nil), df)

	defer df.Close()

	dir := df.(fs.ReadDirFile)
	de, err := dir.ReadDir(1)
	assert.NoError(t, err)

	des := []fs.DirEntry{
		internal.FileInfo("foo", 25, 0o644, time.Time{}, "application/json").(fs.DirEntry),
	}
	assert.EqualValues(t, des, de)

	de, err = dir.ReadDir(2)
	assert.NoError(t, err)

	des = []fs.DirEntry{
		internal.FileInfo("bar", 25, 0o644, time.Time{}, "application/json").(fs.DirEntry),
		internal.DirInfo("bazDir", time.Time{}).(fs.DirEntry),
	}
	assert.EqualValues(t, des, de)

	de, err = dir.ReadDir(1)
	assert.ErrorIs(t, err, io.EOF)
	assert.Len(t, de, 0)

	// open and read everything
	df, err = fsys.Open("foo")
	assert.NoError(t, err)

	defer df.Close()

	dir = df.(fs.ReadDirFile)
	de, err = dir.ReadDir(0)
	assert.NoError(t, err)
	assert.Len(t, de, 3)

	// open and read everything a few times
	df, err = fsys.Open("foo")
	assert.NoError(t, err)

	defer df.Close()

	dir = df.(fs.ReadDirFile)
	de, err = dir.ReadDir(-1)
	assert.NoError(t, err)
	assert.Len(t, de, 3)

	de, err = dir.ReadDir(-1)
	assert.NoError(t, err)
	assert.Len(t, de, 0)

	// open and read too many entries
	df, err = fsys.Open(".")
	assert.NoError(t, err)

	defer df.Close()

	dir = df.(fs.ReadDirFile)
	de, err = dir.ReadDir(8)
	assert.ErrorIs(t, err, io.EOF)
	assert.Len(t, de, 3)
}

func TestSubURL(t *testing.T) {
	fsys := &vaultFS{base: tests.MustURL("https://example.com/v1/secret/")}

	sub, err := fsys.subURL("foo")
	assert.NoError(t, err)
	assert.Equal(t, "https://example.com/v1/secret/foo", sub.String())

	fsys = &vaultFS{base: tests.MustURL("vault:///v1/secret/")}
	sub, err = fsys.subURL("sub/foo?param=foo")
	assert.NoError(t, err)
	assert.Equal(t, "vault:///v1/secret/sub/foo?param=foo", sub.String())
}

func TestStat(t *testing.T) {
	v := newRefCountedClient(fakeVaultServer(t))

	fsys := WithAuthMethod(
		TokenAuthMethod("blargh"),
		newWithVaultClient(tests.MustURL("vault:///secret/"), v),
	)

	f, err := fsys.Open("foo")
	assert.NoError(t, err)

	fi, err := f.Stat()
	assert.NoError(t, err)
	assert.Equal(t, "application/json", fsimpl.ContentType(fi))

	err = f.Close()
	assert.NoError(t, err)

	f, err = fsys.Open("bogus")
	assert.NoError(t, err)

	_, err = f.Stat()
	assert.ErrorIs(t, err, fs.ErrNotExist)

	err = f.Close()
	assert.NoError(t, err)
}

type spyAuthMethod struct {
	t        *testing.T
	loggedin bool
}

var _ AuthMethod = (*spyAuthMethod)(nil)

func (m *spyAuthMethod) Login(_ context.Context, client *api.Client) error {
	// should only ever be called once
	assert.False(m.t, m.loggedin)

	client.SetToken("foo")

	m.loggedin = true

	return nil
}

func (m *spyAuthMethod) Logout(_ context.Context, client *api.Client) error {
	client.ClearToken()

	m.loggedin = false

	return nil
}

func TestFileAuthCaching(t *testing.T) {
	v := newRefCountedClient(fakeVaultServer(t))

	am := &spyAuthMethod{}
	fsys := WithAuthMethod(am, newWithVaultClient(tests.MustURL("vault:///secret/"), v))

	f, err := fsys.Open("foo")
	assert.NoError(t, err)
	assert.False(t, am.loggedin)

	_, err = f.Stat()
	assert.NoError(t, err)
	assert.True(t, am.loggedin)

	_, err = f.Read([]byte{})
	assert.NoError(t, err)
	assert.True(t, am.loggedin)

	f2, err := fsys.Open("bar")
	assert.NoError(t, err)
	assert.True(t, am.loggedin)

	_, err = f2.Read([]byte{})
	assert.NoError(t, err)
	assert.True(t, am.loggedin)

	err = f.Close()
	assert.NoError(t, err)
	// still loggedin because f2 is still open
	assert.True(t, am.loggedin)

	// second call errors without logging out or decrementing reference
	err = f.Close()
	// errors because already closed, but still loggedin because f2 is still open
	assert.Error(t, err)
	assert.True(t, am.loggedin)

	err = f2.Close()
	assert.NoError(t, err)
	assert.False(t, am.loggedin)
}
