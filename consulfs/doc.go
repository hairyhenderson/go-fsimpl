// Package consulfs provides an interface to Hashicorp Consul which allows you
// to interact with the Consul K/V store as a standard filesystem.
//
// This filesystem's behaviour complies with fstest.TestFS.
//
// Usage
//
// To use this filesystem, call New with a base URL. All reads from the
// filesystem are relative to this base URL. The schemes "consul",
// "consul+https", "https", "consul+http", and "http" are supported, though the
// http schemes should only be used in development/test environments. If no
// authority part (host:port) is present in the URL, the $CONSUL_HTTP_ADDR
// environment variable will be used as the Consul server's address.
//
// To scope the filesystem to a specific path, use that path on the URL. For
// example, for a filesystem that can only read from keys prefixed with "foo",
// you could use a URL like:
//
//	consul://consulserver.local/foo/
//
// Note: when scoping URLs to specific paths, the URL must end in "/".
//
// In general, data is read from Consul in the same way as with the Consul CLI -
// that is, the "/kv" prefix is not needed.
//
// See the Consul KV Store docs for more details:
// https://www.consul.io/docs/dynamic-app-config/kv.
//
package consulfs
