// Package vaultfs provides an interface to Hashicorp Vault which allows you to
// interact with the Vault API as a standard filesystem.
//
// This filesystem's behaviour complies with fstest.TestFS.
//
// Usage
//
// To use this filesystem, call New with a base URL. All reads from the
// filesystem are relative to this base URL. The schemes "vault", "vault+https",
// "https", "vault+http", and "http" are supported, though the http schemes
// should only be used in development/test environments. If no authority part
// (host:port) is present in the URL, the $VAULT_ADDR environment variable will
// be used as the Vault server's address.
//
// To scope the filesystem to a specific path, use that path on the URL. For
// example, for a filesystem that can only read from the K/V engine mounted on
// "secret" with a prefix of "dev", you could use a URL like:
//
//	vault:///secret/dev/
//
// Note: when scoping URLs to specific paths, the URL must end in "/".
//
// Reading secrets and credentials from all Vault Secret Engines is supported,
// though some may require parameters to be set. To set parameters, append a
// URL query string to the path when reading (in the form
// "?param=value&param2=value2"). When parameters are set in this way, vaultfs
// will send a POST request to the Vault API, except when the K/V Version 2
// secret engine is in use.
//
// In general, data is read from Vault in the same way as with the Vault CLI -
// that is, the "/v1" prefix is not needed, and with the K/V Version 2 secret
// engine the "data" prefix should not be provided.
//
// See the Vault Secret Engines docs for more details:
// https://vaultproject.io/docs/secrets.
//
// Authentication
//
// A number of authentication methods are supported. By default, the auth method
// will be chosen based on environment variables (using EnvAuthMethod), but
// specific auth methods may be chosen by wrapping the filesystem with the
// WithAuthMethod function.
//
// When multiple files are opened simultaneously, the same authentication token
// will be used for each of them, and will only be revoked when the last file is
// closed. This ensures that a minimal number of tokens are acquired, however
// this also means that tokens may be leaked if all opened files are not closed.
//
// If you wish to manage authentication tokens yourself, you can use the
// TokenAuthMethod and manage that token outside of vaultfs.
//
// See the function documentation for details on each AuthMethod.
//
// For help in deciding which Auth Method to use, consult the Vault
// Documentation: https://vaultproject.io/docs/auth.
//
// Note that only a few auth methods are currently supported by this package. If
// you need additional methods, please file an issue!
//
// Permissions
//
// The correct capabilities must be allowed for the authenticated credentials.
// Regular secret read operations require the "read" capability, dynamic secret
// generation requires "create" and "update", and listing (ReadDir) requires the
// "list" capability.
//
// See https://www.vaultproject.io/docs/concepts/policies#capabilities for more
// details on how to configure these in Vault.
//
// Environment Variables
//
// All auth methods can be configured with the AuthMethod functions, but they
// also each support inferring values from environment variables. See the docs
// for each AuthMethod for details.
//
// In addition, a number of environment variables are understood by the Vault
// client used internally. See the Vault Documentation for details:
// https://vaultproject.io/docs/commands#environment-variables
package vaultfs
