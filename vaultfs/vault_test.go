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

		// defaults to token auth method
		assert.IsType(t, vaultauth.NewTokenAuth(""), vfs.auth)
	}
}

func TestWithContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), struct{ string }{}, "foo")

	fsys := &vaultFS{ctx: context.Background()}
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
	v := newRefCountedClient(fakevault.Server(t))

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
	v := newRefCountedClient(fakevault.Server(t))

	fsys := fs.FS(newWithVaultClient(tests.MustURL("vault:///secret/foo/"), v))
	fsys = WithAuthMethod(TokenAuthMethod("blargh"), fsys)

	de, err := fs.ReadDir(fsys, ".")
	assert.NoError(t, err)

	des := []fs.DirEntry{
		internal.FileInfo("bar", 15, 0o444, time.Time{}, "application/json").(fs.DirEntry),
		internal.DirInfo("bazDir", time.Time{}).(fs.DirEntry),
		internal.FileInfo("foo", 15, 0o444, time.Time{}, "application/json").(fs.DirEntry),
	}
	assert.EqualValues(t, des, de)

	fsys = newWithVaultClient(tests.MustURL("vault:///secret/"), v)
	fsys = WithAuthMethod(TokenAuthMethod("blargh"), fsys)

	de, err = fs.ReadDir(fsys, "foo")
	assert.NoError(t, err)

	des = []fs.DirEntry{
		internal.FileInfo("bar", 15, 0o444, time.Time{}, "application/json").(fs.DirEntry),
		internal.DirInfo("bazDir", time.Time{}).(fs.DirEntry),
		internal.FileInfo("foo", 15, 0o444, time.Time{}, "application/json").(fs.DirEntry),
	}
	assert.EqualValues(t, des, de)
}

//nolint:funlen
func TestReadDirN(t *testing.T) {
	v := newRefCountedClient(fakevault.Server(t))

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
		internal.FileInfo("foo", 15, 0o444, time.Time{}, "application/json").(fs.DirEntry),
	}
	assert.EqualValues(t, des, de)

	de, err = dir.ReadDir(2)
	assert.NoError(t, err)

	des = []fs.DirEntry{
		internal.FileInfo("bar", 15, 0o444, time.Time{}, "application/json").(fs.DirEntry),
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

func TestStat(t *testing.T) {
	v := newRefCountedClient(fakevault.Server(t))

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

func TestFindMountInfo(t *testing.T) {
	testdata := []struct {
		expected    *mountInfo
		mountOpts   interface{}
		mountName   string
		rawFilePath string
	}{
		{
			// no match
			rawFilePath: "/v1/secret/a/b/c", mountName: "potato/",
			mountOpts: map[string]interface{}{
				"type":    "kv",
				"options": map[string]interface{}{"version": "1"},
			}, expected: nil,
		},
		{
			rawFilePath: "/v1/secret/a/b/c", mountName: "secret/",
			mountOpts: map[string]interface{}{
				"type": "kv", "options": map[string]interface{}{"version": "1"},
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
			mountOpts: map[string]interface{}{
				"type": "kv", "options": map[string]interface{}{"version": "2"},
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
	}

	for _, d := range testdata {
		rawMounts := map[string]interface{}{
			"bogus/":    map[string]interface{}{},
			d.mountName: d.mountOpts,
		}

		actual, err := findMountInfo(d.rawFilePath, rawMounts)
		require.NoError(t, err)
		assert.EqualValues(t, d.expected, actual)
	}
}
