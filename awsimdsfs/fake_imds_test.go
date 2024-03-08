package awsimdsfs

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

//nolint:funlen
func fakeIMDSServer(t *testing.T) *httptest.Server {
	t.Helper()

	imdsfsys := fstest.MapFS{
		// directory listings are synthesized - we don't want HTML generated so
		// we provide a fake index.html with just the filenames per line
		"index.html": &fstest.MapFile{Data: []byte("latest/\n")},
		"latest/index.html": &fstest.MapFile{Data: []byte(`dynamic/
meta-data/
user-data`)},
		"latest/dynamic/index.html": &fstest.MapFile{
			Data: []byte("fws/\ninstance-identity/\n"),
		},
		"latest/dynamic/fws/index.html": &fstest.MapFile{
			Data: []byte("instance-monitoring\n"),
		},
		"latest/dynamic/instance-identity/index.html": &fstest.MapFile{
			Data: []byte(`document
pkcs7
signature`),
		},
		"latest/meta-data/index.html": &fstest.MapFile{
			Data: []byte(`ami-id
ami-launch-index
ami-manifest-path
block-device-mapping/
elastic-inference/
events/
hostname
iam/
instance-action
instance-id
instance-life-cycle
instance-type
kernel-id
local-hostname
local-ipv4
mac
network/
placement/
product-codes
public-hostname
public-ipv4
public-keys/
ramdisk-id
reservation-id
security-groups
services/
spot/
tags/
`),
		},
		"latest/meta-data/block-device-mapping/index.html": &fstest.MapFile{
			Data: []byte(`ami
ebs0
ephemeral0
root
swap`),
		},
		"latest/meta-data/elastic-inference/index.html": &fstest.MapFile{
			Data: []byte(`associations/`),
		},
		"latest/meta-data/elastic-inference/associations/index.html": &fstest.MapFile{
			Data: []byte(`eia-bfa21c7904f64a82a21b9f4540169ce1`),
		},
		"latest/meta-data/events/index.html": &fstest.MapFile{
			Data: []byte(`maintenance/
recommendations/`),
		},
		"latest/meta-data/events/maintenance/index.html": &fstest.MapFile{
			Data: []byte(`scheduled`),
		},
		"latest/meta-data/events/recommendations/index.html": &fstest.MapFile{
			Data: []byte(`rebalance`),
		},
		"latest/meta-data/iam/index.html": &fstest.MapFile{
			Data: []byte(`info
security-credentials/`),
		},
		"latest/meta-data/iam/security-credentials/index.html": &fstest.MapFile{
			Data: []byte(`baskinc-role`),
		},
		"latest/meta-data/network/index.html": &fstest.MapFile{
			Data: []byte(`interfaces/`),
		},
		"latest/meta-data/network/interfaces/index.html": &fstest.MapFile{
			Data: []byte(`macs/`),
		},
		"latest/meta-data/network/interfaces/macs/index.html": &fstest.MapFile{
			Data: []byte(`0e:49:61:0f:c3:11/`),
		},
		"latest/meta-data/network/interfaces/macs/0e:49:61:0f:c3:11/index.html": &fstest.MapFile{
			Data: []byte(`device-number
interface-id
ipv4-associations/
ipv6s
local-hostname
local-ipv4s
mac
network-card-index
owner-id
public-hostname
public-ipv4s
security-group-ids
security-groups
subnet-id
subnet-ipv4-cidr-block
subnet-ipv6-cidr-blocks
vpc-id
vpc-ipv4-cidr-block
vpc-ipv4-cidr-blocks
vpc-ipv6-cidr-blocks`),
		},
		"latest/meta-data/network/interfaces/macs/0e:49:61:0f:c3:11/ipv4-associations/index.html": &fstest.MapFile{
			Data: []byte(`192.0.2.54`),
		},
		"latest/meta-data/placement/index.html": &fstest.MapFile{
			Data: []byte(`availability-zone
group-name
host-id
partition-number
region`),
		},
		"latest/meta-data/public-keys/index.html": &fstest.MapFile{
			Data: []byte(`0/`),
		},
		"latest/meta-data/public-keys/0/index.html": &fstest.MapFile{
			Data: []byte(`openssh-key`),
		},
		"latest/meta-data/services/index.html": &fstest.MapFile{
			Data: []byte(`domain
partition`),
		},
		"latest/meta-data/spot/index.html": &fstest.MapFile{
			Data: []byte(`instance-action
termination-time`),
		},
		"latest/meta-data/tags/index.html": &fstest.MapFile{
			Data: []byte(`instance/`),
		},
		"latest/meta-data/tags/instance/index.html": &fstest.MapFile{
			Data: []byte(`Name
Test`),
		},

		"latest/dynamic/fws/instance-monitoring": &fstest.MapFile{Data: []byte("disabled")},
		"latest/dynamic/instance-identity/document": &fstest.MapFile{
			Data: []byte(`{
	"accountId": "0123456789",
	"imageId": "ami-0b69ea66ff7391e80",
	"availabilityZone": "us-east-1f",
	"ramdiskId": null,
	"kernelId": null,
	"devpayProductCodes": null,
	"marketplaceProductCodes": null,
	"version": "2017-09-30",
	"privateIp": "10.0.7.10",
	"billingProducts": null,
	"instanceId": "i-1234567890abcdef0",
	"pendingTime": "2019-10-31T07:02:24Z",
	"architecture": "x86_64",
	"instanceType": "m4.xlarge",
	"region": "us-east-1"
}`),
		},
		"latest/dynamic/instance-identity/pkcs7": &fstest.MapFile{
			Data: []byte(`TESTCSqGSIb3DQEHZqCZPIZCZQExCzZJGgUrDgPCGgUZPIZGCSqGSIb3DQEHZaCZJIZEggHderog
ICJhY2NvdW50SWQiIDogIjUxNTPzNjU5NzP4PCIsCiZgImFyY2hpdGVjdHVyZSIgOiZieDg2XzY0
IirKICZiYXZhaWxhYmlsaXR5Wm9uZSIgOiZidXPtZWFzdC0xYSIsCiZgImJpbGxpbmdQcm9kdWN0
cyIgOiGudWxsLZogICJkZXZrYXlQcm9kdWN0Q29kZXPiIDogbnVsbCrKICZibWFya2V0cGxhY2VQ
cm9kdWN0Q29kZXPiIDogbnVsbCrKICZiaW1hZ2VJZCIgOiZiYW1pLTGhODg3ZTQrPWY3NjU0OTP1
IirKICZiaW5zdGFuY2VJZCIgOiZiaS0rYjU5YTdiN2NlN2UzYmIrYSIsCiZgImluc3RhbmNlVHlr
ZSIgOiZibTQueGxhcmdlIirKICZia2VybmVsSWQiIDogbnVsbCrKICZicGVuZGluZ1RpbWUiIDog
IjIrPjZtPDPtPDJUPjZ6PzY6NThaIirKICZicHJpdmF0ZUlrIiZ6ICIxNzIuPzEuPzQuNDPiLZog
ICJyYW1kaXNrSWQiIDogbnVsbCrKICZicmVnaW9uIiZ6ICJ1cy1lYXN0LTEiLZogICJ2ZXJzaW9u
IiZ6ICIyPDE3LTZ5LTPrIgp9ZZZZZZZZPYIGGTCCZRUCZQEraTGcPQsrCQYDVQQGErJVUzEZPGcG
Z1UECGPQV2FzaGluZ3RvbiGTdGF0ZTEQPZ4GZ1UEGxPHU2VhdHRsZTEgPG4GZ1UEChPXQW1hem9u
IFdlYiGTZXJ2aWNlcyGPTEPCCQCWukjZ5V4aZzZJGgUrDgPCGgUZoF0rGZYJKoZIhvcNZQkDPQsG
CSqGSIb3DQEHZTZcGgkqhkiG9r0GCQUxDxcNPjZrPzZyPjZzNzZ1WjZjGgkqhkiG9r0GCQQxFgQU
N1xlDhvo6cYuGjXZ+mlTW66Ff8rrCQYHKoZIzjgEZrQrPC4CFQCjzjYV1zGUZUxTf6rGO0en/PxR
3ZIVZK589qSkEaslLdCzeX2GnQ6dz9UeZZZZZZZZ`),
		},
		"latest/dynamic/instance-identity/signature": &fstest.MapFile{
			Data: []byte(`TesTTKmBbj+DUw6ut6BOr4mFGpax/k6BhIbsotUHvSIhqv7oKqwB4HZhgGP2Gvcxtz5m3QGUbnwI
hy33GWxjn7+qfZ/GUeZB1Ilc+3rW/P9G/tGxIB3HtqB6q2J6B4DOh6CJiH+BnrHazGW+bJD406Nz
eP9n/rGEGGm0cGEbbeB=`),
		},
		"latest/user-data": &fstest.MapFile{
			Data: []byte("1234,john,reboot,true\n"),
		},
		"latest/meta-data/ami-id":            &fstest.MapFile{Data: []byte("ami-0a887e401f7654935")},
		"latest/meta-data/ami-launch-index":  &fstest.MapFile{Data: []byte("0")},
		"latest/meta-data/ami-manifest-path": &fstest.MapFile{Data: []byte("(unknown)")},

		"latest/meta-data/block-device-mapping/ami":        &fstest.MapFile{Data: []byte("/dev/xvda")},
		"latest/meta-data/block-device-mapping/ebs0":       &fstest.MapFile{Data: []byte("sdb")},
		"latest/meta-data/block-device-mapping/ephemeral0": &fstest.MapFile{Data: []byte("sdb")},
		"latest/meta-data/block-device-mapping/root":       &fstest.MapFile{Data: []byte("/dev/xvda")},
		"latest/meta-data/block-device-mapping/swap":       &fstest.MapFile{Data: []byte("sdcs")},

		"latest/meta-data/elastic-inference/associations/eia-bfa21c7904f64a82a21b9f4540169ce1": &fstest.MapFile{
			//nolint:lll
			Data: []byte(`{"version_2018_04_12":{"elastic-inference-accelerator-id":"eia-bfa21c7904f64a82a21b9f4540169ce1","elastic-inference-accelerator-type":"eia1.medium"}}`),
		},

		"latest/meta-data/events/maintenance/scheduled": &fstest.MapFile{
			Data: []byte(`[
	{
		"Code": "system-reboot",
		"Description": "The instance is scheduled for system-reboot",
		"State": "active",
		"EventId": "instance-event-1234567890abcdef0",
		"NotBefore": "19 Dec 2023 13:56:07 GMT",
		"NotAfter": "26 Dec 2023 13:56:07 GMT",
		"NotBeforeDeadline": "28 Dec 2023 13:56:07 GMT"
	}
]`),
		},
		"latest/meta-data/events/recommendations/rebalance": &fstest.MapFile{Data: []byte("{\n\t\"noticeTime\": \"2023-12-19T17:10:11Z\"\n}")},

		"latest/meta-data/hostname": &fstest.MapFile{Data: []byte("ip-172-16-34-43.ec2.internal")},

		"latest/meta-data/iam/info": &fstest.MapFile{Data: []byte(`{
	"Code": "Success",
	"LastUpdated": "2020-04-02T18:50:40Z",
	"InstanceProfileArn": "arn:aws:iam::896453262835:instance-profile/baskinc-role",
	"InstanceProfileId": "AIPA5BOGHHXZELSK34VU4"
}`)},
		"latest/meta-data/iam/security-credentials/baskinc-role": &fstest.MapFile{Data: []byte(`{
	"Code": "Success",
	"LastUpdated": "2020-04-02T18:50:40Z",
	"Type": "AWS-HMAC",
	"AccessKeyId": "12345678901",
	"SecretAccessKey": "v/12345678901",
	"Token": "TEST92test48TEST+y6RpoTEST92test48TEST/8oWVAiBqTEsT5Ky7ty2tEStxC1T==",
	"Expiration": "2020-04-02T00:49:51Z"
}`)},

		"latest/meta-data/instance-action":     &fstest.MapFile{Data: []byte("none")},
		"latest/meta-data/instance-id":         &fstest.MapFile{Data: []byte("i-1234567890abcdef0")},
		"latest/meta-data/instance-life-cycle": &fstest.MapFile{Data: []byte("on-demand")},
		"latest/meta-data/instance-type":       &fstest.MapFile{Data: []byte("m4.xlarge")},
		"latest/meta-data/kernel-id":           &fstest.MapFile{Data: []byte("aki-5c21674b")},
		"latest/meta-data/local-hostname":      &fstest.MapFile{Data: []byte("ip-172-16-34-43.ec2.internal")},
		"latest/meta-data/local-ipv4":          &fstest.MapFile{Data: []byte("172.16.34.43")},
		"latest/meta-data/mac":                 &fstest.MapFile{Data: []byte("0e:49:61:0f:c3:11")},

		"latest/meta-data/network/interfaces/macs/0e:49:61:0f:c3:11/device-number": &fstest.MapFile{Data: []byte("0")},

		"latest/meta-data/network/interfaces/macs/0e:49:61:0f:c3:11/interface-id": &fstest.MapFile{
			Data: []byte("eni-0f95d3625f5c521cc"),
		},

		"latest/meta-data/network/interfaces/macs/0e:49:61:0f:c3:11/ipv4-associations/192.0.2.54": &fstest.MapFile{
			Data: []byte("192.0.2.54"),
		},

		"latest/meta-data/network/interfaces/macs/0e:49:61:0f:c3:11/ipv6s": &fstest.MapFile{
			Data: []byte("2001:db8:8:4::2"),
		},
		"latest/meta-data/network/interfaces/macs/0e:49:61:0f:c3:11/local-hostname": &fstest.MapFile{
			Data: []byte("ip-172-16-34-43.ec2.internal"),
		},
		"latest/meta-data/network/interfaces/macs/0e:49:61:0f:c3:11/local-ipv4s": &fstest.MapFile{
			Data: []byte("172.16.34.43"),
		},
		"latest/meta-data/network/interfaces/macs/0e:49:61:0f:c3:11/mac": &fstest.MapFile{
			Data: []byte("0e:49:61:0f:c3:11"),
		},
		"latest/meta-data/network/interfaces/macs/0e:49:61:0f:c3:11/network-card-index": &fstest.MapFile{
			Data: []byte("0"),
		},
		"latest/meta-data/network/interfaces/macs/0e:49:61:0f:c3:11/owner-id": &fstest.MapFile{
			Data: []byte("515336597381"),
		},
		"latest/meta-data/network/interfaces/macs/0e:49:61:0f:c3:11/public-hostname": &fstest.MapFile{
			Data: []byte("ec2-192-0-2-54.compute-1.amazonaws.com"),
		},
		"latest/meta-data/network/interfaces/macs/0e:49:61:0f:c3:11/public-ipv4s": &fstest.MapFile{
			Data: []byte("192.0.2.54"),
		},
		"latest/meta-data/network/interfaces/macs/0e:49:61:0f:c3:11/security-group-ids": &fstest.MapFile{
			Data: []byte("sg-0b07f8f6cb485d4df"),
		},
		"latest/meta-data/network/interfaces/macs/0e:49:61:0f:c3:11/security-groups": &fstest.MapFile{
			Data: []byte("ura-launch-wizard-harry-1"),
		},
		"latest/meta-data/network/interfaces/macs/0e:49:61:0f:c3:11/subnet-id": &fstest.MapFile{
			Data: []byte("subnet-0ac62554"),
		},
		"latest/meta-data/network/interfaces/macs/0e:49:61:0f:c3:11/subnet-ipv4-cidr-block": &fstest.MapFile{
			Data: []byte("192.0.2.0/24"),
		},
		"latest/meta-data/network/interfaces/macs/0e:49:61:0f:c3:11/subnet-ipv6-cidr-blocks": &fstest.MapFile{
			Data: []byte("2001:db8::/32"),
		},
		"latest/meta-data/network/interfaces/macs/0e:49:61:0f:c3:11/vpc-id": &fstest.MapFile{
			Data: []byte("vpc-d295a6a7"),
		},
		"latest/meta-data/network/interfaces/macs/0e:49:61:0f:c3:11/vpc-ipv4-cidr-block": &fstest.MapFile{
			Data: []byte("192.0.2.0/24"),
		},
		"latest/meta-data/network/interfaces/macs/0e:49:61:0f:c3:11/vpc-ipv4-cidr-blocks": &fstest.MapFile{
			Data: []byte("192.0.2.0/24"),
		},
		"latest/meta-data/network/interfaces/macs/0e:49:61:0f:c3:11/vpc-ipv6-cidr-blocks": &fstest.MapFile{
			Data: []byte("2001:db8::/32"),
		},

		"latest/meta-data/placement/availability-zone": &fstest.MapFile{Data: []byte("us-east-1a")},
		"latest/meta-data/placement/group-name":        &fstest.MapFile{Data: []byte("a-placement-group")},
		"latest/meta-data/placement/host-id":           &fstest.MapFile{Data: []byte("h-0da999999f9999fb9")},
		"latest/meta-data/placement/partition-number":  &fstest.MapFile{Data: []byte("1")},
		"latest/meta-data/placement/region":            &fstest.MapFile{Data: []byte("us-east-1")},

		"latest/meta-data/product-codes":   &fstest.MapFile{Data: []byte("3iplms73etrdhxdepv72l6ywj")},
		"latest/meta-data/public-hostname": &fstest.MapFile{Data: []byte("ec2-192-0-2-54.compute-1.amazonaws.com")},
		"latest/meta-data/public-ipv4":     &fstest.MapFile{Data: []byte("192.0.2.54")},

		"latest/meta-data/public-keys/0/openssh-key": &fstest.MapFile{
			//nolint:lll
			Data: []byte(`ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQC/JxGByvHDHgQAU+0nRFWdvMPi22OgNUn9ansrI8QN1ZJGxD1ML8DRnJ3Q3zFKqqjGucfNWW0xpVib+ttkIBp8G9P/EOcX9C3FF63O3SnnIUHJsp5faRAZsTJPx0G5HUbvhBvnAcCtSqQgmr02c1l582vAWx48pOmeXXMkl9qe9V/s7K3utmeZkRLo9DqnbsDlg5GWxLC/rWKYaZR66CnMEyZ7yBy3v3abKaGGRovLkHNAgWjSSgmUTI1nT5/S2OLxxuDnsC7+BiABLPaqlIE70SzcWZ0swx68Bo2AY9T9ymGqeAM/1T4yRtg0sPB98TpT7WrY5A3iia2UVtLO/xcTt test`),
		},

		"latest/meta-data/ramdisk-id":         &fstest.MapFile{Data: []byte("ari-01bb5768")},
		"latest/meta-data/reservation-id":     &fstest.MapFile{Data: []byte("r-046cb3eca3e201d2f")},
		"latest/meta-data/security-groups":    &fstest.MapFile{Data: []byte("ura-launch-wizard-harry-1")},
		"latest/meta-data/services/domain":    &fstest.MapFile{Data: []byte("amazonaws.com")},
		"latest/meta-data/services/partition": &fstest.MapFile{Data: []byte("aws")},
		"latest/meta-data/spot/instance-action": &fstest.MapFile{Data: []byte(`{
	"action": "terminate",
	"time": "2023-12-19T17:26:30Z"
}`)},
		"latest/meta-data/spot/termination-time": &fstest.MapFile{Data: []byte("2023-12-19T17:25:44Z")},
		"latest/meta-data/tags/instance/Name":    &fstest.MapFile{Data: []byte("test-instance")},
		"latest/meta-data/tags/instance/Test":    &fstest.MapFile{Data: []byte("test-tag")},
	}

	permRedirectMW := func(pathHandler http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			wrec := httptest.NewRecorder()
			pathHandler.ServeHTTP(wrec, r)

			// try again on 301s - likely just a trailing `/` missing
			// (the AWS client won't follow them, and the real IMDS endpoint
			// doesn't care about /s)
			if wrec.Code == http.StatusMovedPermanently {
				if !strings.HasSuffix(r.URL.Path, "/") {
					r.URL.Path += "/"
				}

				pathHandler.ServeHTTP(w, r)

				return
			}

			for k, v := range wrec.Header() {
				w.Header()[k] = v
			}

			w.WriteHeader(wrec.Code)
			_, _ = w.Write(wrec.Body.Bytes())
		})
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/latest/api/token", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			var err error
			_, err = io.ReadAll(r.Body)
			require.NoError(t, err)

			defer r.Body.Close()
		}

		_, _ = w.Write([]byte("testtoken"))
	}))
	mux.Handle("/", http.FileServer(http.FS(imdsfsys)))

	srv := httptest.NewServer(permRedirectMW(mux))
	t.Cleanup(srv.Close)

	return srv
}
