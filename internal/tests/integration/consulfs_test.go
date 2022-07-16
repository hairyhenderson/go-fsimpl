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
	testClient  *api.Client
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

	tmpDir := gotestfs.NewDir(t, "gofsimpl-inttests",
		gotestfs.WithFile(
			"consul.json",
			`{
				"log_level": "err",
				"primary_datacenter": "dc1",
				"acl": {
					"enabled": true,
					"tokens": {
						"initial_management": "`+consulRootToken+`",
						"default": "x`+consulRootToken+`"
					},
					"default_policy": "deny",
					"enable_token_persistence": true
				},
				"ports": {
					"http": `+strconv.Itoa(httpPort)+`,
					"server": `+strconv.Itoa(serverPort)+`,
					"serf_lan": `+strconv.Itoa(serfLanPort)+`,
					"serf_wan": -1,
					"dns": -1,
					"grpc": -1
				},
				"connect": { "enabled": false }
			}`,
		),
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

		consulResult.Assert(t, icmd.Expected{ExitCode: 0})
	})

	t.Logf("Fired up Consul: %v", consul)

	err := waitForURL(t, "http://"+consulAddr+"/v1/status/leader")
	require.NoError(t, err)

	vaultAddr := startVault(t)

	// create ACL policies & roles, for use in some tests
	cfg := api.DefaultConfig()
	cfg.Address = "http://" + consulAddr
	cfg.Token = consulRootToken
	adminClient, err := api.NewClient(cfg)
	require.NoError(t, err)

	_, _, err = adminClient.ACL().PolicyCreate(&api.ACLPolicy{
		Name:  "readonly",
		Rules: `acl = "read"`,
	}, &api.WriteOptions{Token: consulRootToken})
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
	}, &api.WriteOptions{Token: consulRootToken})
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
	}, &api.WriteOptions{Token: consulRootToken})
	require.NoError(t, err)

	_, _, err = adminClient.ACL().RoleCreate(&api.ACLRole{
		Name: "testrole",
		Policies: []*api.ACLLink{
			{Name: "readonly"},
			{Name: "kvrules"},
		},
	}, &api.WriteOptions{Token: consulRootToken})
	require.NoError(t, err)

	tok, _, err := adminClient.ACL().TokenCreate(&api.ACLToken{
		Roles: []*api.ACLLink{{Name: "testrole"}},
	}, &api.WriteOptions{Token: consulRootToken})
	require.NoError(t, err)

	testcfg := api.DefaultConfig()
	testcfg.Address = "http://" + consulAddr
	testcfg.Token = tok.SecretID
	testClient, err := api.NewClient(testcfg)
	require.NoError(t, err)

	return consulTestConfig{
		consulAddr: consulAddr,
		vaultAddr:  vaultAddr,
		rootToken:  consulRootToken,
		testToken:  tok.SecretID,

		adminClient: adminClient,
		testClient:  testClient,
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

	fsys = consulfs.WithConsulClientFS(tcfg.testClient, fsys)

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

//nolint:funlen
func TestConsulFS_WithVaultAuth(t *testing.T) {
	tcfg := setupConsulFSTest(t)

	vaultClient := adminClient(t, tcfg.vaultAddr)

	err := vaultClient.Sys().Mount("consul/", &vaultapi.MountInput{Type: "consul"})
	require.NoError(t, err)

	//nolint:errcheck
	defer vaultClient.Sys().Unmount("consul/")

	_, err = vaultClient.Logical().Write("consul/config/access", map[string]interface{}{
		"address": tcfg.consulAddr, "token": tcfg.rootToken,
	})
	require.NoError(t, err)
	_, err = vaultClient.Logical().Write("consul/roles/kvrules", map[string]interface{}{
		"policies": "kvrules",
	})
	require.NoError(t, err)

	// Get a handle to the KV API
	kv := tcfg.adminClient.KV()

	t.Cleanup(func() {
		_, _ = kv.DeleteTree("", nil)
	})

	_, _ = kv.Put(&api.KVPair{Key: "vkeys/foo", Value: []byte("bar")}, nil)

	fsys, err := consulfs.New(tests.MustURL("consul://" + tcfg.consulAddr + "/vkeys/"))
	require.NoError(t, err)

	fsys = consulfs.WithConsulClientFS(tcfg.testClient, fsys)

	// this should error as we're not authorized with the correct role
	_, err = fsys.Open("foo")
	require.Error(t, err)

	f, err := fsys.Open("foo")
	require.Error(t, err)

	defer f.Close()

	b, err := io.ReadAll(f)
	require.NoError(t, err)
	assert.Equal(t, "bar", string(b))
}
