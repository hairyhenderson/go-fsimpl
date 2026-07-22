package vaultfs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/hairyhenderson/go-fsimpl"
	"github.com/hairyhenderson/go-fsimpl/internal"
	"github.com/hairyhenderson/go-fsimpl/internal/tests"
	"github.com/hairyhenderson/go-fsimpl/internal/tests/fakevault"
	"github.com/hairyhenderson/go-fsimpl/vaultfs/vaultauth"
	"github.com/hashicorp/vault/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Example() {
	base, _ := url.Parse("vault://my.vaultserver.local:8200")
	token := "1234abcd"

	fsys, _ := New(base)
	fsys = vaultauth.WithAuthMethod(vaultauth.NewTokenAuth(token), fsys)

	b, _ := fs.ReadFile(fsys, "secret/mysecret")

	// data returned by Vault is always JSON
	s := struct{ Value string }{}

	_ = json.Unmarshal(b, &s)

	fmt.Printf("the secret is %s\n", s.Value)
}

func TestVaultConfig(t *testing.T) {
	err := os.Unsetenv("VAULT_ADDR")
	require.NoError(t, err)

	config, err := vaultConfig(tests.MustURL("vault:///"))
	require.NoError(t, err)
	assert.Equal(t, "https://127.0.0.1:8200", config.Address)

	config, err = vaultConfig(tests.MustURL("vault://vault.example.com"))
	require.NoError(t, err)
	assert.Equal(t, "https://vault.example.com", config.Address)
}

func TestNew(t *testing.T) {
	_, err := New(nil)
	require.Error(t, err)

	_, err = New(tests.MustURL("vault:///secret/foo"))
	require.Error(t, err)

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
		require.NoError(t, err)
		require.IsType(t, &vaultFS{}, fsys)

		vfs := fsys.(*vaultFS)
		assert.Equal(t, d.expected, vfs.base.String())

		// defaults to token auth method
		assert.IsType(t, vaultauth.NewTokenAuth(""), vfs.auth)
	}
}

func TestWithContext(t *testing.T) {
	type key struct{}

	ctx := context.WithValue(t.Context(), key{}, "foo")

	fsys := &vaultFS{ctx: t.Context()}
	fsys = fsys.WithContext(ctx).(*vaultFS)

	assert.Same(t, ctx, fsys.ctx)
}

func TestWithHeader(t *testing.T) {
	fsys := &vaultFS{client: newRefCountedClient(fakevault.Server(t))}

	fsys = fsys.WithHeader(http.Header{
		"foo": []string{"bar"},
	}).(*vaultFS)

	assert.Equal(t, "bar", fsys.client.Headers().Get("foo"))

	fsys = &vaultFS{client: newRefCountedClient(fakevault.Server(t))}
	fsys.client.AddHeader("foo", "bar")

	fsys = fsys.WithHeader(http.Header{
		"foo": []string{"bar2"},
		"baz": []string{"qux"},
	}).(*vaultFS)

	assert.Equal(t, []string{"bar", "bar2"}, fsys.client.Headers().Values("foo"))
	assert.Equal(t, "qux", fsys.client.Headers().Get("baz"))
}

func TestOpen(t *testing.T) {
	fsys, err := New(tests.MustURL("vault+https://127.0.0.1:8200/secret/"))
	require.NoError(t, err)

	_, err = fsys.Open("/bogus")
	require.Error(t, err)

	if runtime.GOOS != "windows" {
		_, err = fsys.Open("bo\\gus")
		require.Error(t, err)
	}
}

func jsonMap(b []byte) map[string]string {
	m := map[string]string{}
	_ = json.Unmarshal(b, &m)

	return m
}

func TestReadFile(t *testing.T) {
	expected := "{\"value\":\"foo\"}"
	v := newRefCountedClient(fakevault.Server(t))

	fsys := fs.FS(newWithVaultClient(tests.MustURL("vault:///secret/"), v))
	fsys = WithAuthMethod(TokenAuthMethod("blargh"), fsys)

	f, err := fsys.Open("foo")
	require.NoError(t, err)

	b, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, []byte(expected), b)

	b, err = fs.ReadFile(fsys, "bar")
	require.NoError(t, err)
	assert.Equal(t, []byte(expected), b)

	b, err = fs.ReadFile(fsys, "foo?param=value&method=POST")
	require.NoError(t, err)
	assert.Equal(t,
		map[string]string{"param": "value", "value": "foo"},
		jsonMap(b),
	)

	err = f.Close()
	require.NoError(t, err)
}

func TestReadDirFS(t *testing.T) {
	v := newRefCountedClient(fakevault.Server(t))

	fsys := fs.FS(newWithVaultClient(tests.MustURL("vault:///secret/foo/"), v))
	fsys = WithAuthMethod(TokenAuthMethod("blargh"), fsys)

	de, err := fs.ReadDir(fsys, ".")
	require.NoError(t, err)

	des := []fs.DirEntry{
		internal.FileInfo("bar", 15, 0o444, time.Time{}, "application/json").(fs.DirEntry),
		internal.DirInfo("bazDir", time.Time{}).(fs.DirEntry),
		internal.FileInfo("foo", 15, 0o444, time.Time{}, "application/json").(fs.DirEntry),
	}
	assert.Equal(t, des, de)

	fsys = newWithVaultClient(tests.MustURL("vault:///secret/"), v)
	fsys = WithAuthMethod(TokenAuthMethod("blargh"), fsys)

	de, err = fs.ReadDir(fsys, "foo")
	require.NoError(t, err)

	des = []fs.DirEntry{
		internal.FileInfo("bar", 15, 0o444, time.Time{}, "application/json").(fs.DirEntry),
		internal.DirInfo("bazDir", time.Time{}).(fs.DirEntry),
		internal.FileInfo("foo", 15, 0o444, time.Time{}, "application/json").(fs.DirEntry),
	}
	assert.Equal(t, des, de)
}

//nolint:funlen
func TestReadDirN(t *testing.T) {
	v := newRefCountedClient(fakevault.Server(t))

	fsys := fs.FS(newWithVaultClient(tests.MustURL("vault:///secret/"), v))
	fsys = WithAuthMethod(TokenAuthMethod("blargh"), fsys)

	// open and read a few entries at a time
	df, err := fsys.Open("foo")
	require.NoError(t, err)
	assert.Implements(t, (*fs.ReadDirFile)(nil), df)

	defer df.Close()

	dir := df.(fs.ReadDirFile)
	de, err := dir.ReadDir(1)
	require.NoError(t, err)

	des := []fs.DirEntry{
		internal.FileInfo("foo", 15, 0o444, time.Time{}, "application/json").(fs.DirEntry),
	}
	assert.Equal(t, des, de)

	de, err = dir.ReadDir(2)
	require.NoError(t, err)

	des = []fs.DirEntry{
		internal.FileInfo("bar", 15, 0o444, time.Time{}, "application/json").(fs.DirEntry),
		internal.DirInfo("bazDir", time.Time{}).(fs.DirEntry),
	}
	assert.Equal(t, des, de)

	de, err = dir.ReadDir(1)
	require.ErrorIs(t, err, io.EOF)
	assert.Empty(t, de)

	// open and read everything
	df, err = fsys.Open("foo")
	require.NoError(t, err)

	defer df.Close()

	dir = df.(fs.ReadDirFile)
	de, err = dir.ReadDir(0)
	require.NoError(t, err)
	assert.Len(t, de, 3)

	// open and read everything a few times
	df, err = fsys.Open("foo")
	require.NoError(t, err)

	defer df.Close()

	dir = df.(fs.ReadDirFile)
	de, err = dir.ReadDir(-1)
	require.NoError(t, err)
	assert.Len(t, de, 3)

	de, err = dir.ReadDir(-1)
	require.NoError(t, err)
	assert.Empty(t, de)

	// open and read too many entries
	df, err = fsys.Open(".")
	require.NoError(t, err)

	defer df.Close()

	dir = df.(fs.ReadDirFile)
	de, err = dir.ReadDir(8)
	require.ErrorIs(t, err, io.EOF)
	assert.Len(t, de, 3)
}

func TestStat(t *testing.T) {
	v := newRefCountedClient(fakevault.Server(t))

	fsys := WithAuthMethod(
		TokenAuthMethod("blargh"),
		newWithVaultClient(tests.MustURL("vault:///secret/"), v),
	)

	f, err := fsys.Open("foo")
	require.NoError(t, err)

	fi, err := f.Stat()
	require.NoError(t, err)
	assert.Equal(t, "application/json", fsimpl.ContentType(fi))

	err = f.Close()
	require.NoError(t, err)

	f, err = fsys.Open("bogus")
	require.NoError(t, err)

	_, err = f.Stat()
	require.ErrorIs(t, err, fs.ErrNotExist)

	err = f.Close()
	require.NoError(t, err)
}

type spyAuthMethod struct {
	t      *testing.T
	called bool
}

var _ api.AuthMethod = (*spyAuthMethod)(nil)

func (m *spyAuthMethod) Login(_ context.Context, client *api.Client) (*api.Secret, error) {
	// should only ever be called once, so token should be empty
	require.Empty(m.t, client.Token())
	require.False(m.t, m.called)

	return &api.Secret{Auth: &api.SecretAuth{ClientToken: "foo"}}, nil
}

// make sure logout functionality works
func (m *spyAuthMethod) Logout(_ context.Context, client *api.Client) {
	client.ClearToken()
}

func TestFileAuthCaching(t *testing.T) {
	v := newRefCountedClient(fakevault.Server(t))

	am := &spyAuthMethod{t: t}
	fsys := vaultauth.WithAuthMethod(am, newWithVaultClient(tests.MustURL("vault:///secret/"), v))

	f, err := fsys.Open("foo")
	require.NoError(t, err)
	assert.Empty(t, v.Token())

	_, err = f.Stat()
	require.NoError(t, err)
	assert.Equal(t, "foo", v.Token())

	_, err = f.Read([]byte{})
	require.NoError(t, err)
	assert.Equal(t, "foo", v.Token())

	f2, err := fsys.Open("bar")
	require.NoError(t, err)
	assert.Equal(t, "foo", v.Token())

	_, err = f2.Read([]byte{})
	require.NoError(t, err)
	assert.Equal(t, "foo", v.Token())

	err = f.Close()
	require.NoError(t, err)
	// still loggedin because f2 is still open
	assert.Equal(t, "foo", v.Token())

	// second call errors without logging out or decrementing reference
	err = f.Close()
	// errors because already closed, but still loggedin because f2 is still open
	require.Error(t, err)
	assert.Equal(t, "foo", v.Token())

	err = f2.Close()
	require.NoError(t, err)
	assert.Empty(t, v.Token())
}

//nolint:funlen
func TestFindMountInfo(t *testing.T) {
	testdata := []struct {
		expected    *mountInfo
		mountOpts   any
		mountName   string
		rawFilePath string
	}{
		{
			// no match
			rawFilePath: "/v1/secret/a/b/c", mountName: "potato/",
			mountOpts: map[string]any{
				"type":    "kv",
				"options": map[string]any{"version": "1"},
			}, expected: nil,
		},
		{
			rawFilePath: "/v1/secret/a/b/c", mountName: "secret/",
			mountOpts: map[string]any{
				"type": "kv", "options": map[string]any{"version": "1"},
			},
			expected: &mountInfo{
				secretPath: "/a/b/c",
				name:       "secret/",
				MountOutput: &api.MountOutput{
					Type:    "kv",
					Options: map[string]string{"version": "1"},
				},
			},
		},
		{
			// just the mount, e.g. for list
			rawFilePath: "/v1/kv2", mountName: "kv2/",
			mountOpts: map[string]any{
				"type": "kv", "options": map[string]any{"version": "2"},
			},
			expected: &mountInfo{
				secretPath: "",
				name:       "kv2/",
				MountOutput: &api.MountOutput{
					Type:    "kv",
					Options: map[string]string{"version": "2"},
				},
			},
		},
		{
			// mount on a path with multiple slashes
			rawFilePath: "/v1/k/v", mountName: "k/v/",
			mountOpts: map[string]any{
				"type": "kv", "options": map[string]any{"version": "2"},
			},
			expected: &mountInfo{
				secretPath: "",
				name:       "k/v/",
				MountOutput: &api.MountOutput{
					Type:    "kv",
					Options: map[string]string{"version": "2"},
				},
			},
		},
		{
			// legacy "generic" type with version=2 (pre-Vault 0.9.0, same as kv+v2)
			rawFilePath: "/v1/secret/a/b/c", mountName: "secret/",
			mountOpts: map[string]any{
				"type": mountTypeGeneric, "options": map[string]any{"version": "2"},
			},
			expected: &mountInfo{
				secretPath: "/a/b/c",
				name:       "secret/",
				MountOutput: &api.MountOutput{
					Type:    mountTypeGeneric,
					Options: map[string]string{"version": "2"},
				},
			},
		},
		{
			// path-segment boundary: "kv/" must not match "/v1/kv2/..."
			rawFilePath: "/v1/kv2/secret", mountName: "kv/",
			mountOpts: map[string]any{
				"type": "kv", "options": map[string]any{"version": "2"},
			},
			expected: nil,
		},
	}

	for _, d := range testdata {
		rawMounts := map[string]any{
			"bogus/":    map[string]any{},
			d.mountName: d.mountOpts,
		}

		actual, err := findMountInfo(d.rawFilePath, rawMounts)
		require.NoError(t, err)
		assert.Equal(t, d.expected, actual)
	}
}

// genericV2Server returns a fake Vault server whose mount type is "generic"
// with options[version]="2", simulating a legacy mount that still reports the
// pre-rename type name while using the KV v2 data/metadata path layout.
func genericV2Server(t *testing.T) *api.Client {
	t.Helper()

	// Map from request path to the JSON object returned inside {"data": ...}.
	// LIST paths end with "/".
	// Data paths use the KV v2 response layout: {"data": <payload>, "metadata": <meta>}.
	responses := map[string]map[string]any{
		"/v1/secret/metadata/":     {"keys": []string{"foo", "bar/"}},
		"/v1/secret/metadata/bar/": {"keys": []string{"baz"}},
		"/v1/secret/data/foo": {
			"data":     map[string]any{"value": "foo"},
			"metadata": map[string]any{"version": 1, "deletion_time": ""},
		},
		"/v1/secret/data/bar/baz": {
			"data":     map[string]any{"value": "bar"},
			"metadata": map[string]any{"version": 1, "deletion_time": ""},
		},
	}

	mountH := func(w http.ResponseWriter, _ *http.Request) {
		mounts := map[string]any{
			"secret/": map[string]any{
				"type":    mountTypeGeneric,
				"options": map[string]any{"version": "2"},
			},
		}
		resp := map[string]any{"data": map[string]any{"secret": mounts}}
		_ = json.NewEncoder(w).Encode(resp)
	}

	secretH := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if r.Method == "LIST" || (r.Method == http.MethodGet && r.URL.Query().Get("list") == "true") {
			p += "/"
		}

		data, ok := responses[p]
		if !ok {
			w.WriteHeader(http.StatusNotFound)

			return
		}

		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/sys/internal/ui/mounts", mountH)
	mux.Handle("/", secretH)

	return fakevault.FakeVault(t, mux)
}

func TestReadFile_GenericV2Mount(t *testing.T) {
	v := newRefCountedClient(genericV2Server(t))

	fsys := fs.FS(newWithVaultClient(tests.MustURL("vault:///secret/"), v))
	fsys = WithAuthMethod(TokenAuthMethod("blargh"), fsys)

	b, err := fs.ReadFile(fsys, "foo")
	require.NoError(t, err)
	assert.JSONEq(t, `{"value":"foo"}`, string(b))

	b, err = fs.ReadFile(fsys, "bar/baz")
	require.NoError(t, err)
	assert.JSONEq(t, `{"value":"bar"}`, string(b))
}

func TestReadDirFS_GenericV2Mount(t *testing.T) {
	v := newRefCountedClient(genericV2Server(t))

	fsys := fs.FS(newWithVaultClient(tests.MustURL("vault:///secret/"), v))
	fsys = WithAuthMethod(TokenAuthMethod("blargh"), fsys)

	de, err := fs.ReadDir(fsys, ".")
	require.NoError(t, err)

	names := make([]string, len(de))
	for i, e := range de {
		names[i] = e.Name()
	}

	assert.Equal(t, []string{"bar", "foo"}, names)
}

func TestFindMountInfoWithAuthPrefix(t *testing.T) {
	rawMounts := map[string]any{
		"token/": map[string]any{
			"type": "token",
		},
	}

	actual, err := findMountInfoWithPrefix("/v1/auth/token/lookup-self", rawMounts, "/v1/auth")
	require.NoError(t, err)
	assert.Equal(t, &mountInfo{
		secretPath: "/lookup-self",
		name:       "token/",
		MountOutput: &api.MountOutput{
			Type: "token",
		},
	}, actual)
}

func TestFindMountInfoWithPrefix_ChoosesLongestMatch(t *testing.T) {
	rawMounts := map[string]any{
		"k/": map[string]any{
			"type": "kv",
		},
		"k/v/": map[string]any{
			"type": "kv",
		},
	}

	actual, err := findMountInfoWithPrefix("/v1/k/v/secret", rawMounts, "/v1")
	require.NoError(t, err)
	assert.Equal(t, "k/v/", actual.name)
	assert.Equal(t, "/secret", actual.secretPath)
}

func TestFindMountInfoFromData(t *testing.T) {
	rawData := map[string]any{
		"secret": map[string]any{
			"secret/": map[string]any{
				"type": "kv",
			},
		},
		"auth": map[string]any{
			"token/": map[string]any{
				"type": "token",
			},
		},
	}

	actual, err := findMountInfoFromData("/v1/auth/token/lookup-self", rawData)
	require.NoError(t, err)
	assert.Equal(t, &mountInfo{
		name:       "token/",
		secretPath: "/lookup-self",
		MountOutput: &api.MountOutput{
			Type: "token",
		},
	}, actual)
}

func TestWithConfig(t *testing.T) {
	cl := fakevault.Server(t)

	t.Run("config provided", func(t *testing.T) {
		config := cl.CloneConfig()
		fsys := WithAuthMethod(
			TokenAuthMethod("blargh"),
			// fsys without vault client - will panic unless a client is injected
			newWithVaultClient(tests.MustURL("vault:///secret/"), nil),
		)
		fsys = WithConfig(config, fsys).(*vaultFS)

		f, err := fsys.Open("foo")
		require.NoError(t, err)

		fi, err := f.Stat()
		require.NoError(t, err)
		assert.Equal(t, "application/json", fsimpl.ContentType(fi))
	})

	t.Run("bad config errors with nil fs", func(t *testing.T) {
		config := cl.CloneConfig()
		config.Address = "bad url://"

		vaultFs := newWithVaultClient(tests.MustURL("vault:///secret/"), nil)
		assert.Nil(t, WithConfig(config, vaultFs))
	})

	t.Run("nil config ignored", func(t *testing.T) {
		vaultFs := newWithVaultClient(tests.MustURL("vault:///secret/"), nil)
		fsys := WithConfig(nil, vaultFs)
		assert.Same(t, vaultFs, fsys)
	})

	t.Run("URL with host overrides what's in the config", func(t *testing.T) {
		config := api.DefaultConfig()
		testdata := []struct {
			url, addr string
		}{
			{"vault+https://example.com/secret/", "https://example.com"},
			{"vault://example.com/secret/foo", "https://example.com"},
			{"vault+http://example.com/secret/", "http://example.com"},
		}

		for _, d := range testdata {
			vaultFs := newWithVaultClient(tests.MustURL(d.url), nil)
			vaultFs = WithConfig(config, vaultFs).(*vaultFS)
			assert.Equal(t, d.addr, vaultFs.client.CloneConfig().Address)
		}
	})
}

func TestWithClient(t *testing.T) {
	cl := fakevault.Server(t)

	fsys := WithAuthMethod(
		TokenAuthMethod("blargh"),
		// fsys without vault client - will panic unless a client is injected
		newWithVaultClient(tests.MustURL("vault:///secret/"), nil),
	)
	fsys = WithClient(cl, fsys).(*vaultFS)

	f, err := fsys.Open("foo")
	require.NoError(t, err)

	fi, err := f.Stat()
	require.NoError(t, err)
	assert.Equal(t, "application/json", fsimpl.ContentType(fi))
}
