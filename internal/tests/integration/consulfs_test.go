//go:build !windows

package integration

import (
	"io"
	"strconv"
	"testing"
	"testing/fstest"

	"github.com/hairyhenderson/go-fsimpl/consulfs"
	"github.com/hairyhenderson/go-fsimpl/internal/tests"
	"github.com/hashicorp/consul/api"
	vaultapi "github.com/hashicorp/vault/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gotestfs "gotest.tools/v3/fs"
	"gotest.tools/v3/icmd"
)

const consulRootToken = "00000000-1111-2222-3333-444455556667"

type consulTestConfig struct {
	adminClient *api.Client
	testConfig  *api.Config
	consulAddr  string
	vaultAddr   string
	rootToken   string
	testToken   string
}

//nolint:funlen
func setupConsulFSTest(t *testing.T) consulTestConfig {
	pidDir := gotestfs.NewDir(t, "gofsimpl-inttests-pid")
	t.Cleanup(pidDir.Remove)

	httpPort, consulAddr := freeport(t)
	serverPort, _ := freeport(t)
	serfLanPort, _ := freeport(t)

	t.Logf("Consul ports: http=%d, server=%d, serf_lan=%d", httpPort, serverPort, serfLanPort)

	if httpPort == 0 || serverPort == 0 || serfLanPort == 0 ||
		httpPort == serverPort || httpPort == serfLanPort || serverPort == serfLanPort {
		t.Fatal("failed to find unique free ports")
	}

	consulConfig := `{
	"log_level": "err",
	"primary_datacenter": "dc1",
	"acl": {
		"enabled": true,
		"tokens": {
			"initial_management": "` + consulRootToken + `"
		},
		"default_policy": "deny",
		"enable_token_persistence": false
	},
	"ports": {
		"http": ` + strconv.Itoa(httpPort) + `,
		"server": ` + strconv.Itoa(serverPort) + `,
		"serf_lan": ` + strconv.Itoa(serfLanPort) + `,
		"serf_wan": -1,
		"dns": -1,
		"grpc": -1
	},
	"connect": { "enabled": false }
}`

	tmpDir := gotestfs.NewDir(t, "gofsimpl-inttests",
		gotestfs.WithFile("consul.json", consulConfig),
		gotestfs.WithFile("vault.json", `{
		"pid_file": "`+pidDir.Join("vault.pid")+`"
		}`),
	)
	t.Cleanup(tmpDir.Remove)

	consul := icmd.Command("consul", "agent",
		"-dev",
		"-config-file="+tmpDir.Join("consul.json"),
		"-pid-file="+pidDir.Join("consul.pid"),
	)
	consulResult := icmd.StartCmd(consul)

	t.Cleanup(func() {
		err := consulResult.Cmd.Process.Kill()
		assert.NoError(t, err)

		_ = consulResult.Cmd.Wait()

		t.Logf("consul logs:\n%s\n", consulResult.Combined())

		t.Logf("consul config:\n%s\n", consulConfig)

		consulResult.Assert(t, icmd.Expected{ExitCode: 0})
	})

	t.Logf("Fired up Consul: %v", consul)

	ctx := t.Context()

	err := waitForURL(ctx, t, "http://"+consulAddr+"/v1/status/leader")
	require.NoError(t, err)

	vaultAddr := startVault(ctx, t)

	// create ACL policies & roles, for use in some tests
	cfg := api.DefaultConfig()
	cfg.Address = "http://" + consulAddr
	cfg.Token = consulRootToken
	adminClient, err := api.NewClient(cfg)
	require.NoError(t, err)

	_, _, err = adminClient.ACL().PolicyCreate(&api.ACLPolicy{
		Name:  "readonly",
		Rules: `acl = "read"`,
	}, nil)
	require.NoError(t, err)

	_, _, err = adminClient.ACL().PolicyCreate(&api.ACLPolicy{
		Name: "kvrules",
		Rules: `key_prefix "" {
	policy = "read"
}
key_prefix "vkeys" {
	policy = "deny"
}
`,
	}, nil)
	require.NoError(t, err)

	_, _, err = adminClient.ACL().PolicyCreate(&api.ACLPolicy{
		Name: "vaultpol",
		Rules: `key_prefix "" {
	policy = "deny"
}
key_prefix "vkeys" {
	policy = "read"
}
`,
	}, nil)
	require.NoError(t, err)

	_, _, err = adminClient.ACL().RoleCreate(&api.ACLRole{
		Name: "testrole",
		Policies: []*api.ACLLink{
			{Name: "readonly"},
			{Name: "kvrules"},
		},
	}, nil)
	require.NoError(t, err)

	tok, _, err := adminClient.ACL().TokenCreate(&api.ACLToken{
		Roles: []*api.ACLLink{{Name: "testrole"}},
	}, nil)
	require.NoError(t, err)

	testConfig := api.DefaultConfig()
	testConfig.Address = "http://" + consulAddr
	testConfig.Token = tok.SecretID

	return consulTestConfig{
		consulAddr: consulAddr,
		vaultAddr:  vaultAddr,
		rootToken:  consulRootToken,
		testToken:  tok.SecretID,

		adminClient: adminClient,
		testConfig:  testConfig,
	}
}

func TestConsulFS(t *testing.T) {
	tcfg := setupConsulFSTest(t)

	kv := tcfg.adminClient.KV()

	t.Cleanup(func() {
		_, _ = kv.DeleteTree("", nil)
	})

	_, _ = kv.Put(&api.KVPair{Key: "rootfile", Value: []byte("foo")}, nil)
	_, _ = kv.Put(&api.KVPair{Key: "dir/one", Value: []byte("bar")}, nil)
	_, _ = kv.Put(&api.KVPair{Key: "dir/two", Value: []byte("baz")}, nil)
	_, _ = kv.Put(&api.KVPair{Key: "dir/subdir/three", Value: []byte("qux")}, nil)
	_, _ = kv.Put(&api.KVPair{Key: "dir/subdir/four", Value: []byte("quux")}, nil)
	_, _ = kv.Put(&api.KVPair{Key: "dir/subdir/five", Value: []byte("corge")}, nil)
	_, _ = kv.Put(&api.KVPair{Key: "dir/subdir/six", Value: []byte("grault")}, nil)

	fsys, err := consulfs.New(tests.MustURL("consul+http://" + tcfg.consulAddr))
	require.NoError(t, err)

	fsys = consulfs.WithConfigFS(tcfg.testConfig, fsys)

	err = fstest.TestFS(fsys,
		"rootfile",
		"dir/one",
		"dir/two",
		"dir/subdir/three",
		"dir/subdir/four",
		"dir/subdir/five",
		"dir/subdir/six",
	)
	assert.NoError(t, err)
}

func TestConsulFS_WithVaultAuth(t *testing.T) {
	tcfg := setupConsulFSTest(t)

	vaultClient := adminClient(t, tcfg.vaultAddr)

	err := vaultClient.Sys().Mount("consul/", &vaultapi.MountInput{Type: "consul"})
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = vaultClient.Sys().Unmount("consul/")
	})

	_, err = vaultClient.Logical().Write("consul/config/access", map[string]any{
		"address": tcfg.consulAddr, "token": tcfg.rootToken,
	})
	require.NoError(t, err)
	_, err = vaultClient.Logical().Write("consul/roles/vaultpol", map[string]any{
		"policies": "vaultpol",
	})
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = tcfg.adminClient.KV().DeleteTree("", nil)
	})

	_, _ = tcfg.adminClient.KV().Put(&api.KVPair{Key: "vkeys/foo", Value: []byte("bar")}, nil)

	fsys, err := consulfs.New(tests.MustURL("consul://" + tcfg.consulAddr + "/vkeys/"))
	require.NoError(t, err)

	fsys = consulfs.WithConfigFS(tcfg.testConfig, fsys)

	// open doesn't error as no read is attempted yet
	f, err := fsys.Open("foo")
	require.NoError(t, err)

	t.Cleanup(func() { _ = f.Close() })

	// this should error as we're not authorized with the correct role
	_, err = io.ReadAll(f)
	require.Error(t, err)

	fsys = consulfs.WithTokenFS(getVaultToken(t, tcfg, "consul/creds/vaultpol"), fsys)
	f, err = fsys.Open("foo")
	require.NoError(t, err)

	t.Cleanup(func() { _ = f.Close() })

	// this should work as we're authorized with the correct role from Vault
	b, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, "bar", string(b))
}

func getVaultToken(t *testing.T, tcfg consulTestConfig, credPath string) string {
	vaultClient := adminClient(t, tcfg.vaultAddr)
	secret, err := vaultClient.Logical().Read(credPath)
	require.NoError(t, err)

	return secret.Data["token"].(string)
}

func TestConsulFS_WithQueryOptions(t *testing.T) {
	tcfg := setupConsulFSTest(t)

	_, _ = tcfg.adminClient.KV().Put(&api.KVPair{Key: "foo", Value: []byte("bar")}, nil)

	fsys, err := consulfs.New(tests.MustURL("consul://" + tcfg.consulAddr + "/"))
	require.NoError(t, err)

	fsys = consulfs.WithConfigFS(tcfg.testConfig, fsys)

	// open doesn't error as no read is attempted yet
	f, err := fsys.Open("foo")
	require.NoError(t, err)

	t.Cleanup(func() { _ = f.Close() })

	// this should error as we're not authorized with the correct role
	b, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, "bar", string(b))

	// use query options to query with a different token, which should error
	fsys2 := consulfs.WithQueryOptionsFS(&api.QueryOptions{Token: "bogus"}, fsys)
	f, err = fsys2.Open("foo")
	require.NoError(t, err)

	t.Cleanup(func() { _ = f.Close() })

	// this should error as we're no longer authorized
	_, err = io.ReadAll(f)
	require.Error(t, err)

	// the original fsys should still work - WithQueryOptionsFS copies fsys
	f, err = fsys.Open("foo")
	require.NoError(t, err)

	t.Cleanup(func() { _ = f.Close() })

	b, err = io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, "bar", string(b))
}
