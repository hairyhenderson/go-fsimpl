package gcpmetafs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsMetadataDirectory(t *testing.T) {
	directories := []string{
		"instance",
		"instance/",
		"/instance/",
		"instance/attributes",
		"instance/disks",
		"instance/disks/0",
		"instance/network-interfaces",
		"instance/network-interfaces/0",
		"instance/network-interfaces/0/access-configs",
		"instance/network-interfaces/0/access-configs/0",
		"instance/network-interfaces/0/forwarded-ips",
		"instance/service-accounts",
		"instance/service-accounts/default",
		"project",
		"project/",
		"project/attributes",
	}
	for _, d := range directories {
		assert.True(t, isMetadataDirectory(d), d)
	}

	files := []string{
		"instance/id",
		"instance/hostname",
		"instance/cpu-platform",
		"instance/machine-type",
		"instance/zone",
		"instance/attributes/enable-oslogin",
		"instance/disks/0/device-name",
		"instance/network-interfaces/0/ip",
		"instance/service-accounts/default/email",
		"project/project-id",
		"project/numeric-project-id",
		"project/attributes/ssh-keys",
	}
	for _, f := range files {
		assert.False(t, isMetadataDirectory(f), f)
	}
}
