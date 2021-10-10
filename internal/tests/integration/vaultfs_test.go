//go:build !windows

package integration

import (
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/hairyhenderson/go-fsimpl/internal/tests"
	"github.com/hairyhenderson/go-fsimpl/vaultfs"
	"github.com/hashicorp/vault/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tfs "gotest.tools/v3/fs"
	"gotest.tools/v3/icmd"
)

const vaultRootToken = "00000000-1111-2222-3333-444455556666"

func setupVaultFSTest(t *testing.T) string {
	addr := startVault(t)

	t.Helper()

	client := adminClient(t, addr)

	err := client.Sys().PutPolicy("writepol", `path "*" {
  capabilities = ["create","update","delete"]
}`)
	require.NoError(t, err)
	err = client.Sys().PutPolicy("readpol", `path "*" {
  capabilities = ["read","delete"]
}`)
	require.NoError(t, err)
	err = client.Sys().PutPolicy("listpol", `path "*" {
  capabilities = ["read","list","delete"]
}`)
	require.NoError(t, err)

	return addr
}

func adminClient(t *testing.T, addr string) *api.Client {
	os.Setenv("VAULT_ADDR", "http://"+addr)
	defer os.Unsetenv("VAULT_ADDR")

	client, err := api.NewClient(nil)
	require.NoError(t, err)

	client.SetToken(vaultRootToken)

	return client
}

func tokenCreate(client *api.Client, policy string, uses int) (string, error) {
	opts := &api.TokenCreateRequest{
		Policies: []string{policy},
		TTL:      "1m",
		NumUses:  uses,
	}

	token, err := client.Auth().Token().Create(opts)
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

	_, vaultAddr := freeport()
	vault := icmd.Command("vault", "server",
		"-dev",
		"-dev-root-token-id="+vaultRootToken,
		"-dev-leased-kv",
		"-log-level=err",
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
	addr := setupVaultFSTest(t)

	client := adminClient(t, addr)

	_, _ = client.Logical().Write("secret/one", map[string]interface{}{"value": "foo"})
	_, _ = client.Logical().Write("secret/dir/two", map[string]interface{}{"value": 42})
	_, _ = client.Logical().Write("secret/dir/three", map[string]interface{}{"value": 43})
	_, _ = client.Logical().Write("secret/dir/four", map[string]interface{}{"value": 44})
	_, _ = client.Logical().Write("secret/dir/five", map[string]interface{}{"value": 45})

	fsys, _ := vaultfs.New(tests.MustURL("vault+http://" + addr + "/secret/"))
	fsys = vaultfs.WithAuthMethod(vaultfs.TokenAuthMethod(vaultRootToken), fsys)

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
	addr := setupVaultFSTest(t)

	client := adminClient(t, addr)

	_, _ = client.Logical().Write("secret/foo", map[string]interface{}{"value": "bar"})

	tok, err := tokenCreate(client, "readpol", 4)
	require.NoError(t, err)

	// address provided, token provided
	fsys, err := vaultfs.New(tests.MustURL("vault+http://" + addr))
	assert.NoError(t, err)

	fsys = vaultfs.WithAuthMethod(vaultfs.TokenAuthMethod(tok), fsys)

	b, err := fs.ReadFile(fsys, "secret/foo")
	assert.NoError(t, err)
	assert.Equal(t, `{"value":"bar"}`, string(b))

	// token in env var
	os.Setenv("VAULT_TOKEN", tok)
	defer os.Unsetenv("VAULT_TOKEN")

	fsys, err = vaultfs.New(tests.MustURL("vault+http://" + addr))
	assert.NoError(t, err)

	b, err = fs.ReadFile(fsys, "secret/foo")
	assert.NoError(t, err)
	assert.Equal(t, `{"value":"bar"}`, string(b))

	// address and token in env var
	os.Setenv("VAULT_ADDR", "http://"+addr)
	defer os.Unsetenv("VAULT_ADDR")

	fsys, err = vaultfs.New(tests.MustURL("vault:///"))
	assert.NoError(t, err)

	b, err = fs.ReadFile(fsys, "secret/foo")
	assert.NoError(t, err)
	assert.Equal(t, `{"value":"bar"}`, string(b))
}

//nolint:funlen
func TestVaultFS_UserPassAuth(t *testing.T) {
	addr := setupVaultFSTest(t)

	client := adminClient(t, addr)

	_, _ = client.Logical().Write("secret/foo", map[string]interface{}{"value": "bar"})
	_, _ = client.Logical().Write("secret/dir/foo", map[string]interface{}{"value": "dir"})
	_, _ = client.Logical().Write("secret/dir/bar", map[string]interface{}{"value": "dir"})

	opts := &api.EnableAuthOptions{Type: "userpass"}
	err := client.Sys().EnableAuthWithOptions("userpass", opts)
	require.NoError(t, err)

	err = client.Sys().EnableAuthWithOptions("userpass2", opts)
	require.NoError(t, err)

	_, err = client.Logical().Write("auth/userpass/users/dave",
		map[string]interface{}{
			"password": "foo", "ttl": "1000s", "policies": "listpol",
		})
	require.NoError(t, err)

	_, err = client.Logical().Write("auth/userpass2/users/dave",
		map[string]interface{}{
			"password": "bar", "ttl": "10s", "policies": "readpol",
		})
	require.NoError(t, err)

	fsys, err := vaultfs.New(tests.MustURL("http://" + addr + "/secret/"))
	assert.NoError(t, err)

	fsys = vaultfs.WithAuthMethod(
		vaultfs.UserPassAuthMethod("dave", "foo", ""), fsys)

	b, err := fs.ReadFile(fsys, "foo")
	assert.NoError(t, err)
	assert.Equal(t, `{"value":"bar"}`, string(b))

	// should only have the root token remaining (Close should logout and revoke
	// token)
	list, err := client.Logical().List("auth/token/accessors")
	require.NoError(t, err)
	assert.Len(t, list.Data["keys"], 1)

	// now with the other mount point
	fsys, err = vaultfs.New(tests.MustURL("http://" + addr + "/secret/"))
	assert.NoError(t, err)

	fsys = vaultfs.WithAuthMethod(
		vaultfs.UserPassAuthMethod("dave", "bar", "userpass2"), fsys)

	b, err = fs.ReadFile(fsys, "foo")
	assert.NoError(t, err)
	assert.Equal(t, `{"value":"bar"}`, string(b))

	// with a bunch of env vars
	os.Setenv("VAULT_ADDR", "http://"+addr)
	os.Setenv("VAULT_AUTH_USERNAME", "dave")
	os.Setenv("VAULT_AUTH_PASSWORD", "foo")

	defer os.Unsetenv("VAULT_ADDR")
	defer os.Unsetenv("VAULT_AUTH_USERNAME")
	defer os.Unsetenv("VAULT_AUTH_PASSWORD")

	fsys, err = vaultfs.New(tests.MustURL("vault:///secret/"))
	assert.NoError(t, err)

	f, err := fsys.Open("foo")
	assert.NoError(t, err)

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
	list, err = client.Logical().List("auth/token/accessors")
	require.NoError(t, err)
	assert.Len(t, list.Data["keys"], 1)
}

//nolint:errcheck,funlen
func TestVaultFS_AppRoleAuth(t *testing.T) {
	addr := setupVaultFSTest(t)

	client := adminClient(t, addr)

	_, _ = client.Logical().Write("secret/foo", map[string]interface{}{"value": "bar"})
	defer client.Logical().Delete("secret/foo")

	err := client.Sys().EnableAuth("approle", "approle", "")
	require.NoError(t, err)
	err = client.Sys().EnableAuth("approle2", "approle", "")
	require.NoError(t, err)

	defer client.Sys().DisableAuth("approle")
	defer client.Sys().DisableAuth("approle2")
	_, err = client.Logical().Write("auth/approle/role/testrole", map[string]interface{}{
		"secret_id_ttl": "10s", "token_ttl": "20s",
		"secret_id_num_uses": "1", "policies": "readpol",
		"token_type": "batch",
	})
	require.NoError(t, err)
	_, err = client.Logical().Write("auth/approle2/role/testrole", map[string]interface{}{
		"secret_id_ttl": "10s", "token_ttl": "20s",
		"secret_id_num_uses": "1", "policies": "readpol",
	})
	require.NoError(t, err)

	rid, _ := client.Logical().Read("auth/approle/role/testrole/role-id")
	roleID := rid.Data["role_id"].(string)
	sid, _ := client.Logical().Write("auth/approle/role/testrole/secret-id", nil)
	secretID := sid.Data["secret_id"].(string)

	fsys, err := vaultfs.New(tests.MustURL("http://" + addr + "/secret/"))
	assert.NoError(t, err)

	fsys = vaultfs.WithAuthMethod(
		vaultfs.AppRoleAuthMethod(roleID, secretID, ""), fsys,
	)

	f, err := fsys.Open("foo")
	assert.NoError(t, err)

	b, err := io.ReadAll(f)
	assert.NoError(t, err)
	assert.Equal(t, `{"value":"bar"}`, string(b))

	err = f.Close()
	assert.NoError(t, err)

	// should only have the root token remaining (Close should logout and revoke
	// token)
	list, err := client.Logical().List("auth/token/accessors")
	require.NoError(t, err)
	assert.Len(t, list.Data["keys"], 1)

	// now with the other mount point
	rid, _ = client.Logical().Read("auth/approle2/role/testrole/role-id")
	roleID = rid.Data["role_id"].(string)
	sid, _ = client.Logical().Write("auth/approle2/role/testrole/secret-id", nil)
	secretID = sid.Data["secret_id"].(string)

	fsys, err = vaultfs.New(tests.MustURL("http://" + addr + "/secret/"))
	assert.NoError(t, err)

	fsys = vaultfs.WithAuthMethod(
		vaultfs.AppRoleAuthMethod(roleID, secretID, "approle2"), fsys,
	)

	b, err = fs.ReadFile(fsys, "foo")
	assert.NoError(t, err)
	assert.Equal(t, `{"value":"bar"}`, string(b))
}

//nolint:errcheck,funlen
func TestVaultFS_AppRoleAuth_ReusedToken(t *testing.T) {
	addr := setupVaultFSTest(t)

	client := adminClient(t, addr)

	_, _ = client.Logical().Write("secret/foo", map[string]interface{}{"value": "foobar"})
	defer client.Logical().Delete("secret/foo")

	_, _ = client.Logical().Write("secret/bar", map[string]interface{}{"value": "barbar"})
	defer client.Logical().Delete("secret/bar")

	_, _ = client.Logical().Write("secret/baz", map[string]interface{}{"value": "bazbar"})
	defer client.Logical().Delete("secret/baz")

	err := client.Sys().EnableAuth("approle", "approle", "")
	require.NoError(t, err)

	defer client.Sys().DisableAuth("approle")
	_, err = client.Logical().Write("auth/approle/role/testrole", map[string]interface{}{
		"secret_id_ttl": "10s", "token_ttl": "20s",
		"secret_id_num_uses": "1", "policies": "readpol",
	})
	require.NoError(t, err)

	rid, _ := client.Logical().Read("auth/approle/role/testrole/role-id")
	roleID := rid.Data["role_id"].(string)
	sid, _ := client.Logical().Write("auth/approle/role/testrole/secret-id", nil)
	secretID := sid.Data["secret_id"].(string)

	fsys, err := vaultfs.New(tests.MustURL("http://" + addr + "/secret/"))
	assert.NoError(t, err)

	fsys = vaultfs.WithAuthMethod(
		vaultfs.AppRoleAuthMethod(roleID, secretID, ""), fsys,
	)

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

//nolint:errcheck
func TestVaultFS_AppIDAuth(t *testing.T) {
	addr := setupVaultFSTest(t)

	client := adminClient(t, addr)

	client.Logical().Write("secret/foo", map[string]interface{}{"value": "bar"})
	defer client.Logical().Delete("secret/foo")
	err := client.Sys().EnableAuth("app-id", "app-id", "")
	require.NoError(t, err)
	err = client.Sys().EnableAuth("app-id2", "app-id", "")
	require.NoError(t, err)

	defer client.Sys().DisableAuth("app-id")
	defer client.Sys().DisableAuth("app-id2")
	_, err = client.Logical().Write("auth/app-id/map/app-id/testappid", map[string]interface{}{
		"display_name": "test_app_id", "value": "readpol",
	})
	require.NoError(t, err)
	_, err = client.Logical().Write("auth/app-id/map/user-id/testuserid", map[string]interface{}{
		"value": "testappid",
	})
	require.NoError(t, err)
	_, err = client.Logical().Write("auth/app-id2/map/app-id/testappid", map[string]interface{}{
		"display_name": "test_app_id", "value": "readpol",
	})
	require.NoError(t, err)
	_, err = client.Logical().Write("auth/app-id2/map/user-id/testuserid", map[string]interface{}{
		"value": "testappid",
	})
	require.NoError(t, err)

	fsys, err := vaultfs.New(tests.MustURL("http://" + addr))
	assert.NoError(t, err)

	//nolint:staticcheck
	fsys = vaultfs.WithAuthMethod(vaultfs.AppIDAuthMethod("testappid", "testuserid", ""), fsys)

	b, err := fs.ReadFile(fsys, "secret/foo")
	assert.NoError(t, err)
	assert.Equal(t, `{"value":"bar"}`, string(b))
}

func TestVaultFS_DynamicAuth(t *testing.T) {
	addr := setupVaultFSTest(t)

	client := adminClient(t, addr)

	err := client.Sys().Mount("ssh/", &api.MountInput{Type: "ssh"})
	require.NoError(t, err)

	_, err = client.Logical().Write("ssh/roles/test", map[string]interface{}{
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

	tok, err := tokenCreate(client, "writepol", len(testCommands)*2)
	require.NoError(t, err)

	for _, d := range testCommands {
		d := d
		t.Run(d.url, func(t *testing.T) {
			fsys, err := vaultfs.New(tests.MustURL("http://" + addr + d.url))
			assert.NoError(t, err)

			fsys = vaultfs.WithAuthMethod(vaultfs.TokenAuthMethod(tok), fsys)

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
	addr := setupVaultFSTest(t)

	client := adminClient(t, addr)

	_, _ = client.Logical().Write("secret/dir/foo", map[string]interface{}{"value": "one"})
	_, _ = client.Logical().Write("secret/dir/bar", map[string]interface{}{"value": "two"})

	tok, err := tokenCreate(client, "listpol", 5)
	require.NoError(t, err)

	fsys, err := vaultfs.New(tests.MustURL("http://" + addr + "/secret/dir/"))
	assert.NoError(t, err)

	fsys = vaultfs.WithAuthMethod(vaultfs.TokenAuthMethod(tok), fsys)

	de, err := fs.ReadDir(fsys, ".")
	assert.NoError(t, err)
	assert.Len(t, de, 2)

	assert.Equal(t, "bar", de[0].Name())
	assert.Equal(t, "foo", de[1].Name())
}
