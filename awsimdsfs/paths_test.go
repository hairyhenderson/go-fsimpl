package awsimdsfs

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsIMDSDirectory(t *testing.T) {
	directories := []string{
		"meta-data",
		"meta-data/",
		"/meta-data/",
		"meta-data/autoscaling",
		"meta-data/block-device-mapping",
		"meta-data/events",
		"meta-data/events/maintenance",
		"meta-data/events/recommendations",
		"meta-data/iam/",
		"meta-data/iam/security-credentials",
		"meta-data/identity-credentials",
		"meta-data/identity-credentials/ec2/",
		"meta-data/identity-credentials/ec2/security-credentials",
		"meta-data/metrics",
		"meta-data/network",
		"meta-data/network/interfaces",
		"meta-data/network/interfaces/macs/",
		"meta-data/placement",
		"meta-data/public-keys",
		"meta-data/public-keys/0",
		"meta-data/services",
		"meta-data/spot",
		"meta-data/tags",
		"dynamic",
		"dynamic/",
		"dynamic/fws",
		"dynamic/instance-identity/",

		"network/interfaces/macs/foo/",
		"network/interfaces/macs/00:00:00:00:00:00",
		"network/interfaces/macs/12:34:56:ab:cd:ef/ipv4-associations/",
	}
	for _, d := range directories {
		assert.True(t, isIMDSDirectory(d), d)
	}

	files := []string{
		"user-data",
		"meta-data/iam/security-credentials/ec2-instance",
	}
	for _, f := range files {
		assert.False(t, isIMDSDirectory(f), f)
	}
}
