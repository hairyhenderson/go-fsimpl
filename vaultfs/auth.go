package vaultfs

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/hairyhenderson/go-fsimpl/internal/env"
	"github.com/hashicorp/vault/api"
)

// withAuthMethoder is an fs.FS that can be configured with a custom Vault Client
type withAuthMethoder interface {
	WithAuthMethod(auth AuthMethod) fs.FS
}

// AuthMethod is an authentication method that vaultfs can use to acquire a
// token.
type AuthMethod interface {
	// Login acquires a Vault token using client for communicating with Vault,
	// and configures client with the token.
	Login(ctx context.Context, client *api.Client) error

	// Logout revokes the Vault token attached to client.
	Logout(ctx context.Context, client *api.Client) error
}

// WithAuthMethod configures the given FS to authenticate with auth, if the
// filesystem supports it.
//
// Note that this is not required if $VAULT_TOKEN is set.
func WithAuthMethod(auth AuthMethod, fsys fs.FS) fs.FS {
	if afsys, ok := fsys.(withAuthMethoder); ok {
		return afsys.WithAuthMethod(auth)
	}

	return fsys
}

var (
	_ AuthMethod = (*envAuthMethod)(nil)
	_ AuthMethod = (*appIDAuthMethod)(nil)
	_ AuthMethod = (*appRoleAuthMethod)(nil)
	_ AuthMethod = (*gitHubAuthMethod)(nil)
	_ AuthMethod = (*userPassAuthMethod)(nil)
)

// EnvAuthMethod chooses the first auth method to have the correct environment
// variables set, in this order of precedence:
//
//	AppRoleAuthMethod
//	GitHubAuthMethod
//	UserPassAuthMethod
//	TokenAuthMethod
//	AppIDAuthMethod	// Deprecated
func EnvAuthMethod() AuthMethod {
	return &envAuthMethod{
		// sorted in order of precedence
		methods: []AuthMethod{
			AppRoleAuthMethod("", "", ""),
			GitHubAuthMethod("", ""),
			UserPassAuthMethod("", "", ""),
			TokenAuthMethod(""),
			AppIDAuthMethod("", "", ""),
		},
	}
}

type envAuthMethod struct {
	chosen  AuthMethod
	methods []AuthMethod
}

func (m *envAuthMethod) Login(ctx context.Context, client *api.Client) (err error) {
	if m.chosen == nil {
		for _, auth := range m.methods {
			err = auth.Login(ctx, client)
			if err == nil {
				m.chosen = auth

				break
			}
		}
	}

	if m.chosen == nil {
		return fmt.Errorf("unable to authenticate with vault by any configured method. Last error was: %w", err)
	}

	return nil
}

func (m *envAuthMethod) Logout(ctx context.Context, client *api.Client) (err error) {
	// reset so we can login again
	defer func() { m.chosen = nil }()

	if m.chosen != nil {
		err = m.chosen.Logout(ctx, client)
		if err == nil {
			return nil
		}
	}

	return fmt.Errorf("unable to authenticate with vault by any configured method. Last error was: %w", err)
}

// TokenAuthMethod authenticates with the given token, or if none is provided,
// attempts to read from the $VAULT_TOKEN environment variable, or the
// $HOME/.vault-token file.
//
// See also https://www.vaultproject.io/docs/auth/token
func TokenAuthMethod(token string) AuthMethod {
	return &tokenAuthMethod{token: token, fsys: os.DirFS("/")}
}

type tokenAuthMethod struct {
	fsys  fs.FS
	token string
}

func (m *tokenAuthMethod) Login(ctx context.Context, client *api.Client) error {
	token := findValue(m.token, "VAULT_TOKEN", "", m.fsys)
	if token != "" {
		client.SetToken(token)

		return nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	p := path.Join(homeDir, ".vault-token")
	p = strings.TrimPrefix(p, "/")

	b, err := fs.ReadFile(m.fsys, p)
	if err != nil {
		return fmt.Errorf("failed to read .vault-token file from %q: %w", homeDir, err)
	}

	client.SetToken(string(b))

	return nil
}

func (m *tokenAuthMethod) Logout(ctx context.Context, client *api.Client) error {
	// just clear the client's token, nothing else needs to be done here
	client.ClearToken()

	return nil
}

// AppRoleAuthMethod authenticates to Vault with the AppRole auth method. If
// either roleID or secretID are omitted, the values will be read from the
// $VAULT_ROLE_ID and/or $VAULT_SECRET_ID environment variables.
//
// If mount is not set, it defaults to the value of $VAULT_AUTH_APPROLE_MOUNT
// or "approle".
//
// See also https://www.vaultproject.io/docs/auth/approle
func AppRoleAuthMethod(roleID, secretID, mount string) AuthMethod {
	return &appRoleAuthMethod{
		fsys:     os.DirFS("/"),
		roleID:   roleID,
		secretID: secretID,
		mount:    mount,
	}
}

type appRoleAuthMethod struct {
	fsys             fs.FS
	roleID, secretID string
	mount            string
}

func (m *appRoleAuthMethod) Login(ctx context.Context, client *api.Client) error {
	roleID := findValue(m.roleID, "VAULT_ROLE_ID", "", m.fsys)
	if roleID == "" {
		return fmt.Errorf("approle auth failure: no role_id provided")
	}

	secretID := findValue(m.secretID, "VAULT_SECRET_ID", "", m.fsys)
	if secretID == "" {
		return fmt.Errorf("approle auth failure: no secret_id provided")
	}

	mount := findValue(m.mount, "VAULT_AUTH_APPROLE_MOUNT", "approle", m.fsys)

	secret, err := remoteAuth(ctx, client, mount, "",
		map[string]string{"role_id": roleID, "secret_id": secretID})
	if err != nil {
		return fmt.Errorf("approle login failed: %w", err)
	}

	client.SetToken(secret.Auth.ClientToken)

	return nil
}

func (m *appRoleAuthMethod) Logout(ctx context.Context, client *api.Client) error {
	return revokeToken(ctx, client)
}

// AppIDAuthMethod authenticates to Vault with the AppID auth method.
//
// Deprecated: transition to AppRole instead - see https://www.vaultproject.io/docs/auth/app-id
func AppIDAuthMethod(appID, userID, mount string) AuthMethod {
	return &appIDAuthMethod{
		fsys:   os.DirFS("/"),
		appID:  appID,
		userID: userID,
		mount:  mount,
	}
}

type appIDAuthMethod struct {
	fsys          fs.FS
	appID, userID string
	mount         string
}

//nolint:dupl
func (m *appIDAuthMethod) Login(ctx context.Context, client *api.Client) error {
	appID := findValue(m.appID, "VAULT_APP_ID", "", m.fsys)
	if appID == "" {
		return fmt.Errorf("app-id auth failure: no app_id provided")
	}

	userID := findValue(m.userID, "VAULT_USER_ID", "", m.fsys)
	if userID == "" {
		return fmt.Errorf("app-id auth failure: no user_id provided")
	}

	mount := findValue(m.mount, "VAULT_AUTH_APP_ID_MOUNT", "app-id", m.fsys)

	secret, err := remoteAuth(ctx, client, mount, appID,
		map[string]string{"user_id": userID})
	if err != nil {
		return fmt.Errorf("app-id login failed: %w", err)
	}

	client.SetToken(secret.Auth.ClientToken)

	return nil
}

func (m *appIDAuthMethod) Logout(ctx context.Context, client *api.Client) error {
	return revokeToken(ctx, client)
}

// GitHubAuthMethod authenticates to Vault with the GitHub auth method. If
// ghtoken is omitted, its value will be read from the $VAULT_AUTH_GITHUB_TOKEN
// environment variable.
//
// If mount is not set, it defaults to the value of $VAULT_AUTH_GITHUB_MOUNT
// or "github".
//
// See also https://www.vaultproject.io/docs/auth/github
func GitHubAuthMethod(ghtoken, mount string) AuthMethod {
	return &gitHubAuthMethod{
		fsys:    os.DirFS("/"),
		ghtoken: ghtoken,
		mount:   mount,
	}
}

type gitHubAuthMethod struct {
	fsys    fs.FS
	ghtoken string
	mount   string
}

func (m *gitHubAuthMethod) Login(ctx context.Context, client *api.Client) error {
	ghtoken := findValue(m.ghtoken, "VAULT_AUTH_GITHUB_TOKEN", "", m.fsys)
	if ghtoken == "" {
		return fmt.Errorf("github auth failure: no username provided")
	}

	mount := findValue(m.mount, "VAULT_AUTH_GITHUB_MOUNT", "github", m.fsys)

	secret, err := remoteAuth(ctx, client, mount, "",
		map[string]string{"token": ghtoken})
	if err != nil {
		return fmt.Errorf("github login failed: %w", err)
	}

	client.SetToken(secret.Auth.ClientToken)

	return nil
}

func (m *gitHubAuthMethod) Logout(ctx context.Context, client *api.Client) error {
	return revokeToken(ctx, client)
}

// UserPassAuthMethod authenticates to Vault with the UpserPass auth method. If
// either username or password are omitted, the values will be read from the
// $VAULT_AUTH_USERNAME and/or $VAULT_AUTH_PASSWORD environment variables.
//
// If mount is not set, it defaults to the value of $VAULT_AUTH_USERPASS_MOUNT
// or "userpass".
//
// See also https://www.vaultproject.io/docs/auth/userpass
func UserPassAuthMethod(username, password, mount string) AuthMethod {
	return &userPassAuthMethod{
		fsys:     os.DirFS("/"),
		username: username,
		password: password,
		mount:    mount,
	}
}

type userPassAuthMethod struct {
	fsys               fs.FS
	username, password string
	mount              string
}

//nolint:dupl
func (m *userPassAuthMethod) Login(ctx context.Context, client *api.Client) error {
	username := findValue(m.username, "VAULT_AUTH_USERNAME", "", m.fsys)
	if username == "" {
		return fmt.Errorf("userpass auth failure: no username provided")
	}

	password := findValue(m.password, "VAULT_AUTH_PASSWORD", "", m.fsys)
	if password == "" {
		return fmt.Errorf("userpass auth failure: no password provided")
	}

	mount := findValue(m.mount, "VAULT_AUTH_USERPASS_MOUNT", "userpass", m.fsys)

	secret, err := remoteAuth(ctx, client, mount, username,
		map[string]string{"password": password})
	if err != nil {
		return fmt.Errorf("userpass login failed: %w", err)
	}

	client.SetToken(secret.Auth.ClientToken)

	return nil
}

func (m *userPassAuthMethod) Logout(ctx context.Context, client *api.Client) error {
	return revokeToken(ctx, client)
}

func findValue(s, envvar, def string, fsys fs.FS) string {
	if s == "" {
		s = env.GetenvFS(fsys, envvar)
	}

	if s == "" {
		s = def
	}

	return s
}

func remoteAuth(ctx context.Context, client *api.Client, mount, extra string, vars map[string]string) (*api.Secret, error) {
	p := fmt.Sprintf("/v1/auth/%s/login", mount)
	p = path.Join(p, extra)

	req := client.NewRequest(http.MethodPut, p)

	err := req.SetJSONBody(vars)
	if err != nil {
		return nil, err
	}

	// equivalent to client.Logical().Write(), but with support for ctx
	resp, err := client.RawRequestWithContext(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("vault write to %s failed: %w", p, vaultFSError(err))
	}
	defer resp.Body.Close()

	secret, err := api.ParseSecret(resp.Body)
	if err != nil {
		return nil, err
	}

	if secret == nil || secret.Auth == nil {
		return nil, fmt.Errorf("invalid response from vault write")
	}

	return secret, nil
}

func revokeToken(ctx context.Context, client *api.Client) error {
	r := client.NewRequest("POST", "/v1/auth/token/revoke-self")

	resp, err := client.RawRequestWithContext(ctx, r)
	if err == nil {
		resp.Body.Close()
	}

	client.ClearToken()

	return err
}
