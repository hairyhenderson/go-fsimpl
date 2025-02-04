// Package sopsfs provides a read-only filesystem for SOPS-encrypted files.
//
// This filesystem only supports single-file operations.
// As such, this filesystem's behaviour does not comply with [testing/fstest.TestFS].
//
// # Usage
//
// To use this filesystem, call New with a base URL. All reads from the
// filesystem are relative to this base host. Valid schemes are 'sops'
//
// # URL Format
//
// The scheme, path, and query are used by this filesystem.
//
// # Path is a path to the SOPS-encrypted file
//
// Query can be used to specify output format ('yaml', 'json')
package sopsfs
