package consulfs

import (
	"io/fs"
)

// withAuthMethoder is an fs.FS that can be configured to use a given Consul
// Auth Method
type withAuthMethoder interface {
	WithAuthMethod(auth AuthMethod) fs.FS
}

// AuthMethod is an authentication method that consulfs can use to acquire a
// token.
type AuthMethod interface {
}
