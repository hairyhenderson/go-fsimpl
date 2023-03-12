//go:build !windows

package integration

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/hairyhenderson/go-fsimpl"
	"github.com/hairyhenderson/go-fsimpl/internal/tests"
	"github.com/hairyhenderson/go-fsimpl/vaultfs"
	"github.com/hairyhenderson/go-fsimpl/vaultfs/vaultauth"
	"github.com/hashicorp/vault/api"
	"github.com/hashicorp/vault/api/auth/approle"
	"github.com/hashicorp/vault/api/auth/userpass"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tfs "gotest.tools/v3/fs"
	"gotest.tools/v3/icmd"
)

const vaultRootToken = "00000000-1111-2222-3333-444455556666"

func setupVaultFSTest(ctx context.Context, t *testing.T) string {
	addr := startVault(t)

	t.Helper()

	client := adminClient(t, addr)

	err := client.Sys().PutPolicyWithContext(ctx, "writepol",
		`path "*" {
			capabilities = ["create","update","delete"]
		}`)
	require.NoError(t, err)
	err = client.Sys().PutPolicyWithContext(ctx, "readpol",
		`path "*" {
			capabilities = ["read","delete"]
		}`)
	require.NoError(t, err)
	err = client.Sys().PutPolicyWithContext(ctx, "listpol",
		`path "*" {
			capabilities = ["read","list","delete"]
		}`)
	require.NoError(t, err)

	return addr
}

func adminClient(t *testing.T, addr string) *api.Client {
	client, err := api.NewClient(&api.Config{Address: "http://" + addr})
	require.NoError(t, err)

	client.SetToken(vaultRootToken)

	return client
}

func tokenCreate(ctx context.Context, client *api.Client, policy string, uses int) (string, error) {
	opts := &api.TokenCreateRequest{
		Policies: []string{policy},
		TTL:      "1m",
		NumUses:  uses,
	}

	token, err := client.Auth().Token().CreateWithContext(ctx, opts)
	if err != nil {
		return "", err
	}

	return token.Auth.ClientToken, nil
}

func startVault(t *testing.T) string {
	pidDir := tfs.NewDir(t, "gofsimpl-inttests-vaultpid")
	t.Cleanup(pidDir.Remove)

	tmpDir := tfs.NewDir(t, "gofsimpl-inttests",
		tfs.WithFile("config.json", `{
		"pid_file": "`+pidDir.Join("vault.pid")+`"
		}`),
	)
	t.Cleanup(tmpDir.Remove)

	// rename any existing token so it doesn't get overridden
	u, _ := user.Current()
	homeDir := u.HomeDir
	tokenFile := path.Join(homeDir, ".vault-token")

	info, err := os.Stat(tokenFile)
	if err == nil && info.Mode().IsRegular() {
		_ = os.Rename(tokenFile, path.Join(homeDir, ".vault-token.bak"))
	}

	_, vaultAddr := freeport(t)
	vault := icmd.Command("vault", "server",
		"-dev",
		"-dev-root-token-id="+vaultRootToken,
		"-dev-kv-v1", // default to v1, so we can test v1 and v2
		"-log-level=error",
		"-dev-listen-address="+vaultAddr,
		"-config="+tmpDir.Join("config.json"),
	)
	result := icmd.StartCmd(vault)

	t.Logf("Fired up Vault: %v", vault)

	err = waitForURL(t, "http://"+vaultAddr+"/v1/sys/health")
	require.NoError(t, err)

	t.Cleanup(func() {
		err := result.Cmd.Process.Kill()
		require.NoError(t, err)

		_ = result.Cmd.Wait()

		result.Assert(t, icmd.Expected{ExitCode: 0})
		t.Logf("Vault output: %s", result.Combined())

		// restore old token if it was backed up
		u, _ := user.Current()
		homeDir := u.HomeDir
		tokenFile := path.Join(homeDir, ".vault-token.bak")

		info, err := os.Stat(tokenFile)
		if err == nil && info.Mode().IsRegular() {
			_ = os.Rename(tokenFile, path.Join(homeDir, ".vault-token"))
		}
	})

	return vaultAddr
}

func TestVaultFS(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addr := setupVaultFSTest(ctx, t)

	client := adminClient(t, addr)

	_, _ = client.Logical().WriteWithContext(ctx, "secret/one", map[string]interface{}{"value": "foo"})
	_, _ = client.Logical().WriteWithContext(ctx, "secret/dir/two", map[string]interface{}{"value": 42})
	_, _ = client.Logical().WriteWithContext(ctx, "secret/dir/three", map[string]interface{}{"value": 43})
	_, _ = client.Logical().WriteWithContext(ctx, "secret/dir/four", map[string]interface{}{"value": 44})
	_, _ = client.Logical().WriteWithContext(ctx, "secret/dir/five", map[string]interface{}{"value": 45})

	fsys, _ := vaultfs.New(tests.MustURL("vault+http://" + addr + "/secret/"))
	fsys = vaultauth.WithAuthMethod(vaultauth.NewTokenAuth(vaultRootToken), fsys)
	fsys = fsimpl.WithContextFS(ctx, fsys)

	err := fstest.TestFS(fsys,
		"one",
		filepath.Join("dir", "two"),
		filepath.Join("dir", "three"),
		filepath.Join("dir", "four"),
		filepath.Join("dir", "five"),
	)
	assert.NoError(t, err)
}

func TestVaultFS_TokenAuth(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addr := setupVaultFSTest(ctx, t)

	client := adminClient(t, addr)
	kv1 := client.KVv1("secret")

	_ = kv1.Put(ctx, "foo", map[string]interface{}{"value": "bar"})

	tok, err := tokenCreate(ctx, client, "readpol", 4)
	require.NoError(t, err)

	// address provided, token provided
	fsys, err := vaultfs.New(tests.MustURL("vault+http://" + addr))
	assert.NoError(t, err)

	fsys = vaultauth.WithAuthMethod(vaultauth.NewTokenAuth(tok), fsys)
	fsys = fsimpl.WithContextFS(ctx, fsys)

	b, err := fs.ReadFile(fsys, "secret/foo")
	assert.NoError(t, err)
	assert.Equal(t, `{"value":"bar"}`, string(b))

	// token in env var
	t.Setenv("VAULT_TOKEN", tok)

	fsys, err = vaultfs.New(tests.MustURL("vault+http://" + addr))
	assert.NoError(t, err)

	fsys = fsimpl.WithContextFS(ctx, fsys)

	b, err = fs.ReadFile(fsys, "secret/foo")
	assert.NoError(t, err)
	assert.Equal(t, `{"value":"bar"}`, string(b))

	// address and token in env var
	t.Setenv("VAULT_ADDR", "http://"+addr)

	fsys, err = vaultfs.New(tests.MustURL("vault:///"))
	assert.NoError(t, err)

	fsys = fsimpl.WithContextFS(ctx, fsys)

	b, err = fs.ReadFile(fsys, "secret/foo")
	assert.NoError(t, err)
	assert.Equal(t, `{"value":"bar"}`, string(b))
}

//nolint:funlen
func TestVaultFS_UserPassAuth(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addr := setupVaultFSTest(ctx, t)

	client := adminClient(t, addr)
	kv1 := client.KVv1("secret")

	_ = kv1.Put(ctx, "foo", map[string]interface{}{"value": "bar"})
	_ = kv1.Put(ctx, "dir/foo", map[string]interface{}{"value": "dir"})
	_ = kv1.Put(ctx, "dir/bar", map[string]interface{}{"value": "dir"})

	opts := &api.EnableAuthOptions{Type: "userpass"}
	err := client.Sys().EnableAuthWithOptionsWithContext(ctx, "userpass", opts)
	require.NoError(t, err)

	err = client.Sys().EnableAuthWithOptionsWithContext(ctx, "userpass2", opts)
	require.NoError(t, err)

	_, err = client.Logical().WriteWithContext(ctx, "auth/userpass/users/dave",
		map[string]interface{}{
			"password": "foo", "ttl": "1000s", "policies": "listpol",
		})
	require.NoError(t, err)

	_, err = client.Logical().WriteWithContext(ctx, "auth/userpass2/users/dave",
		map[string]interface{}{
			"password": "bar", "ttl": "10s", "policies": "readpol",
		})
	require.NoError(t, err)

	fsys, err := vaultfs.New(tests.MustURL("http://" + addr + "/secret/"))
	assert.NoError(t, err)

	upauth, err := userpass.NewUserpassAuth("dave", &userpass.Password{FromString: "foo"})
	require.NoError(t, err)

	fsys = vaultauth.WithAuthMethod(upauth, fsys)
	fsys = fsimpl.WithContextFS(ctx, fsys)

	b, err := fs.ReadFile(fsys, "foo")
	assert.NoError(t, err)
	assert.Equal(t, `{"value":"bar"}`, string(b))

	// should only have the root token remaining (Close should logout and revoke
	// token)
	list, err := client.Logical().ListWithContext(ctx, "auth/token/accessors")
	require.NoError(t, err)
	assert.Len(t, list.Data["keys"], 1)

	// now with the other mount point
	fsys, err = vaultfs.New(tests.MustURL("http://" + addr + "/secret/"))
	assert.NoError(t, err)

	upauth, err = userpass.NewUserpassAuth("dave",
		&userpass.Password{FromString: "bar"}, userpass.WithMountPath("userpass2"))
	require.NoError(t, err)

	fsys = vaultauth.WithAuthMethod(upauth, fsys)
	fsys = fsimpl.WithContextFS(ctx, fsys)

	b, err = fs.ReadFile(fsys, "foo")
	assert.NoError(t, err)
	assert.Equal(t, `{"value":"bar"}`, string(b))

	// with a bunch of env vars
	t.Setenv("VAULT_ADDR", "http://"+addr)
	t.Setenv("VAULT_AUTH_USERNAME", "dave")
	t.Setenv("VAULT_AUTH_PASSWORD", "foo")

	fsys, err = vaultfs.New(tests.MustURL("vault:///secret/"))
	assert.NoError(t, err)

	fsys = fsimpl.WithContextFS(ctx, fsys)

	f, err := fsys.Open("foo")
	assert.NoError(t, err)

	fi, err := f.Stat()
	assert.NoError(t, err)
	assert.Equal(t, int64(15), fi.Size())

	b, err = io.ReadAll(f)
	assert.NoError(t, err)
	assert.Equal(t, `{"value":"bar"}`, string(b))

	b, err = fs.ReadFile(fsys, "foo")
	assert.NoError(t, err)
	assert.Equal(t, `{"value":"bar"}`, string(b))

	b, err = fs.ReadFile(fsys, "foo")
	assert.NoError(t, err)
	assert.Equal(t, `{"value":"bar"}`, string(b))

	dir, err := fsys.Open("dir")
	assert.NoError(t, err)

	de, err := dir.(fs.ReadDirFile).ReadDir(-1)
	assert.NoError(t, err)
	assert.Len(t, de, 2)

	de, err = fs.ReadDir(fsys, "dir")
	assert.NoError(t, err)
	assert.Len(t, de, 2)

	// make sure files are closed so the token will be revoked
	err = dir.Close()
	assert.NoError(t, err)

	err = f.Close()
	assert.NoError(t, err)

	// should only have the root token remaining (Close should logout and revoke
	// token)
	list, err = client.Logical().ListWithContext(ctx, "auth/token/accessors")
	require.NoError(t, err)
	assert.Len(t, list.Data["keys"], 1)
}

//nolint:errcheck,funlen
func TestVaultFS_AppRoleAuth(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addr := setupVaultFSTest(ctx, t)

	client := adminClient(t, addr)
	kv1 := client.KVv1("secret")

	_ = kv1.Put(ctx, "foo", map[string]interface{}{"value": "bar"})
	defer kv1.Delete(ctx, "foo")

	opts := &api.EnableAuthOptions{Type: "approle"}
	err := client.Sys().EnableAuthWithOptionsWithContext(ctx, "approle", opts)
	require.NoError(t, err)
	err = client.Sys().EnableAuthWithOptionsWithContext(ctx, "approle2", opts)
	require.NoError(t, err)

	defer client.Sys().DisableAuthWithContext(ctx, "approle")
	defer client.Sys().DisableAuthWithContext(ctx, "approle2")
	_, err = client.Logical().WriteWithContext(ctx, "auth/approle/role/testrole", map[string]interface{}{
		"secret_id_ttl": "10s", "token_ttl": "20s",
		"secret_id_num_uses": "1", "policies": "readpol",
		"token_type": "batch",
	})
	require.NoError(t, err)
	_, err = client.Logical().WriteWithContext(ctx, "auth/approle2/role/testrole", map[string]interface{}{
		"secret_id_ttl": "10s", "token_ttl": "20s",
		"secret_id_num_uses": "1", "policies": "readpol",
	})
	require.NoError(t, err)

	rid, _ := client.Logical().ReadWithContext(ctx, "auth/approle/role/testrole/role-id")
	roleID := rid.Data["role_id"].(string)
	sid, _ := client.Logical().WriteWithContext(ctx, "auth/approle/role/testrole/secret-id", nil)
	secretID := sid.Data["secret_id"].(string)

	fsys, err := vaultfs.New(tests.MustURL("http://" + addr + "/secret/"))
	assert.NoError(t, err)

	apauth, err := approle.NewAppRoleAuth(roleID, &approle.SecretID{FromString: secretID})
	require.NoError(t, err)

	fsys = vaultauth.WithAuthMethod(apauth, fsys)
	fsys = fsimpl.WithContextFS(ctx, fsys)

	f, err := fsys.Open("foo")
	assert.NoError(t, err)

	b, err := io.ReadAll(f)
	assert.NoError(t, err)
	assert.Equal(t, `{"value":"bar"}`, string(b))

	err = f.Close()
	assert.NoError(t, err)

	// should only have the root token remaining (Close should logout and revoke
	// token)
	list, err := client.Logical().ListWithContext(ctx, "auth/token/accessors")
	require.NoError(t, err)
	assert.Len(t, list.Data["keys"], 1)

	// now with the other mount point
	rid, _ = client.Logical().ReadWithContext(ctx, "auth/approle2/role/testrole/role-id")
	roleID = rid.Data["role_id"].(string)
	sid, _ = client.Logical().WriteWithContext(ctx, "auth/approle2/role/testrole/secret-id", nil)
	secretID = sid.Data["secret_id"].(string)

	fsys, err = vaultfs.New(tests.MustURL("http://" + addr + "/secret/"))
	assert.NoError(t, err)

	apauth, err = approle.NewAppRoleAuth(roleID, &approle.SecretID{FromString: secretID},
		approle.WithMountPath("approle2"))
	require.NoError(t, err)

	fsys = vaultauth.WithAuthMethod(apauth, fsys)
	fsys = fsimpl.WithContextFS(ctx, fsys)

	b, err = fs.ReadFile(fsys, "foo")
	assert.NoError(t, err)
	assert.Equal(t, `{"value":"bar"}`, string(b))
}

//nolint:errcheck,funlen
func TestVaultFS_AppRoleAuth_ReusedToken(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addr := setupVaultFSTest(ctx, t)

	client := adminClient(t, addr)
	kv1 := client.KVv1("secret")

	_ = kv1.Put(ctx, "foo", map[string]interface{}{"value": "foobar"})
	defer kv1.Delete(ctx, "foo")

	_ = kv1.Put(ctx, "bar", map[string]interface{}{"value": "barbar"})
	defer kv1.Delete(ctx, "bar")

	_ = kv1.Put(ctx, "baz", map[string]interface{}{"value": "bazbar"})
	defer kv1.Delete(ctx, "baz")

	err := client.Sys().EnableAuthWithOptionsWithContext(ctx, "approle",
		&api.EnableAuthOptions{Type: "approle"})
	require.NoError(t, err)

	defer client.Sys().DisableAuthWithContext(ctx, "approle")
	_, err = client.Logical().WriteWithContext(ctx, "auth/approle/role/testrole",
		map[string]interface{}{
			"secret_id_ttl": "10s", "token_ttl": "20s",
			"secret_id_num_uses": "1", "policies": "readpol",
		})
	require.NoError(t, err)

	rid, _ := client.Logical().ReadWithContext(ctx, "auth/approle/role/testrole/role-id")
	roleID := rid.Data["role_id"].(string)
	sid, _ := client.Logical().WriteWithContext(ctx, "auth/approle/role/testrole/secret-id", nil)
	secretID := sid.Data["secret_id"].(string)

	fsys, err := vaultfs.New(tests.MustURL("http://" + addr + "/secret/"))
	assert.NoError(t, err)

	apauth, err := approle.NewAppRoleAuth(roleID, &approle.SecretID{FromString: secretID})
	require.NoError(t, err)

	fsys = vaultauth.WithAuthMethod(apauth, fsys)
	fsys = fsimpl.WithContextFS(ctx, fsys)

	// open 4 files simultaneously, and one of them twice
	f1, err := fsys.Open("foo")
	assert.NoError(t, err)

	f2, err := fsys.Open("bar")
	assert.NoError(t, err)

	f3, err := fsys.Open("baz")
	assert.NoError(t, err)

	f4, err := fsys.Open("foo")
	assert.NoError(t, err)

	b, err := io.ReadAll(f1)
	assert.NoError(t, err)
	assert.Equal(t, `{"value":"foobar"}`, string(b))

	b, err = io.ReadAll(f2)
	assert.NoError(t, err)
	assert.Equal(t, `{"value":"barbar"}`, string(b))

	err = f1.Close()
	assert.NoError(t, err)

	err = f2.Close()
	assert.NoError(t, err)

	b, err = io.ReadAll(f3)
	assert.NoError(t, err)
	assert.Equal(t, `{"value":"bazbar"}`, string(b))

	b, err = io.ReadAll(f4)
	assert.NoError(t, err)
	assert.Equal(t, `{"value":"foobar"}`, string(b))

	err = f3.Close()
	assert.NoError(t, err)

	err = f4.Close()
	assert.NoError(t, err)
}

func TestVaultFS_DynamicAuth(t *testing.T) {
	t.Skip("broken?")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addr := setupVaultFSTest(ctx, t)

	client := adminClient(t, addr)

	err := client.Sys().MountWithContext(ctx, "ssh/", &api.MountInput{Type: "ssh"})
	require.NoError(t, err)

	_, err = client.Logical().WriteWithContext(ctx, "ssh/roles/test",
		map[string]interface{}{
			"key_type": "otp", "default_user": "user", "cidr_list": "10.0.0.0/8",
		})
	require.NoError(t, err)

	testCommands := []struct {
		url, path string
	}{
		{"/", "ssh/creds/test?ip=10.1.2.3&username=user"},
		{"/ssh/", "creds/test?ip=10.1.2.3&username=user"},
		{"/ssh/creds/", "test?ip=10.1.2.3&username=user"},
	}

	tok, err := tokenCreate(ctx, client, "writepol", len(testCommands)*2)
	require.NoError(t, err)

	for _, d := range testCommands {
		d := d
		t.Run(d.url, func(t *testing.T) {
			fsys, err := vaultfs.New(tests.MustURL("http://" + addr + d.url))
			assert.NoError(t, err)

			fsys = vaultauth.WithAuthMethod(vaultauth.NewTokenAuth(tok), fsys)
			fsys = fsimpl.WithContextFS(ctx, fsys)

			b, err := fs.ReadFile(fsys, d.path)
			assert.NoError(t, err)

			data := map[string]interface{}{}
			err = json.Unmarshal(b, &data)
			assert.NoError(t, err)

			assert.Equal(t, "10.1.2.3", data["ip"])
		})
	}
}

func TestVaultFS_List(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addr := setupVaultFSTest(ctx, t)

	client := adminClient(t, addr)

	kv1 := client.KVv1("secret")
	_ = kv1.Put(ctx, "dir/foo", map[string]interface{}{"value": "one"})
	_ = kv1.Put(ctx, "dir/bar", map[string]interface{}{"value": "two"})

	tok, err := tokenCreate(ctx, client, "listpol", 5)
	require.NoError(t, err)

	fsys, err := vaultfs.New(tests.MustURL("http://" + addr + "/secret/dir/"))
	assert.NoError(t, err)

	fsys = vaultauth.WithAuthMethod(vaultauth.NewTokenAuth(tok), fsys)
	fsys = fsimpl.WithContextFS(ctx, fsys)

	de, err := fs.ReadDir(fsys, ".")
	assert.NoError(t, err)
	assert.Len(t, de, 2)

	assert.Equal(t, "bar", de[0].Name())
	assert.Equal(t, "foo", de[1].Name())
}

func TestVaultFS_KVv2(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	addr := setupVaultFSTest(ctx, t)
	client := adminClient(t, addr)

	err := client.Sys().MountWithContext(ctx, "kv2", &api.MountInput{Type: "kv",
		Options: map[string]string{"version": "2"},
	})
	require.NoError(t, err)

	s, err := client.KVv2("kv2").Put(ctx, "foo",
		map[string]interface{}{"first": "one"}, api.WithCheckAndSet(0))
	require.NoError(t, err)
	require.Equal(t, 1, s.VersionMetadata.Version)

	s, err = client.KVv2("kv2").Put(ctx, "foo",
		map[string]interface{}{"second": "two"}, api.WithCheckAndSet(1))
	require.NoError(t, err)
	require.Equal(t, 2, s.VersionMetadata.Version)

	tok, err := tokenCreate(ctx, client, "readpol", 5)
	require.NoError(t, err)

	fsys, err := vaultfs.New(tests.MustURL("http://" + addr))
	require.NoError(t, err)

	fsys = vaultauth.WithAuthMethod(vaultauth.NewTokenAuth(tok), fsys)
	fsys = fsimpl.WithContextFS(ctx, fsys)

	b, err := fs.ReadFile(fsys, "kv2/foo")
	require.NoError(t, err)
	assert.Equal(t, `{"second":"two"}`, string(b))

	f, err := fsys.Open("kv2/foo")
	require.NoError(t, err)

	fi, err := f.Stat()
	require.NoError(t, err)
	assert.Equal(t, "application/json", fsimpl.ContentType(fi))
	assert.Equal(t, int64(16), fi.Size())

	v2Time := fi.ModTime()
	assert.NotEqual(t, time.Time{}, v2Time)

	// version 1 should be available
	f, err = fsys.Open("kv2/foo?version=1")
	require.NoError(t, err)

	b, err = io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, `{"first":"one"}`, string(b))

	fi, err = f.Stat()
	require.NoError(t, err)
	assert.Equal(t, "application/json", fsimpl.ContentType(fi))
	assert.Equal(t, int64(15), fi.Size())

	// v1 should have an earlier mod time than v2
	assert.NotEqual(t, v2Time, fi.ModTime())
}
