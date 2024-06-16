package internal

import (
	"testing"

	"github.com/hairyhenderson/go-fsimpl/internal/tests"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubURL(t *testing.T) {
	base := tests.MustURL("https://example.com/dir/")
	sub, err := SubURL(base, "sub")
	assert.NoError(t, err)
	assert.Equal(t, "https://example.com/dir/sub", sub.String())

	base = tests.MustURL("consul:///dir/")
	sub, err = SubURL(base, "sub/foo?param=foo")
	assert.NoError(t, err)
	assert.Equal(t, "consul:///dir/sub/foo?param=foo", sub.String())

	base = tests.MustURL("vault:///dir/?param1=foo&param2=bar")
	sub, err = SubURL(base, "sub/foo")
	require.NoError(t, err)
	assert.Equal(t, "vault:///dir/sub/foo?param1=foo&param2=bar", sub.String())

	base = tests.MustURL("consul:///dir/?param1=foo&param2=bar")
	sub, err = SubURL(base, "sub/foo?param3=baz")
	require.NoError(t, err)
	assert.Equal(t, "consul:///dir/sub/foo?param1=foo&param2=bar&param3=baz", sub.String())
}
