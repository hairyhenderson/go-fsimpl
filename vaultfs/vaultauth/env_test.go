package vaultauth

import (
	"context"
	"testing"

	"github.com/hairyhenderson/go-fsimpl/internal/tests/fakevault"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnvAuthLogin(t *testing.T) {
	v := fakevault.Server(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	t.Setenv("VAULT_TOKEN", "foo")

	m := EnvAuthMethod()
	s, err := m.Login(ctx, v)
	require.NoError(t, err)
	assert.Equal(t, "foo", s.Auth.ClientToken)
	assert.NotNil(t, m.(*compositeAuthMethod).chosen)
}
