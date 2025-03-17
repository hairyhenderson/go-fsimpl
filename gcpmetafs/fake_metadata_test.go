package gcpmetafs

import (
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// fakeMetadataServer creates an HTTP server that serves mock metadata
// The Google metadata client will talk to this server instead of the real metadata service.
func fakeMetadataServer(t *testing.T) *httptest.Server {
	t.Helper()

	metafsys := createMockFS()
	mux := http.NewServeMux()

	// Handle the computeMetadata requests
	mux.HandleFunc("/computeMetadata/v1/", handleMetadataRequest(metafsys))

	// Handle the special token URL that the client uses to authenticate
	mux.HandleFunc("/computeMetadata/v1/instance/service-accounts/default/token", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Metadata-Flavor") != "Google" {
			w.WriteHeader(http.StatusForbidden)

			return
		}

		_, _ = w.Write([]byte(`{"access_token":"fake-token","expires_in":3600,"token_type":"Bearer"}`))
	})

	srv := httptest.NewServer(checkMetadataFlavor(mux))
	t.Cleanup(srv.Close)

	// Set the environment variable so the real client uses our test server
	// Trim the http:// prefix as the client expects just the host:port
	metadataHost := strings.TrimPrefix(srv.URL, "http://")
	t.Setenv("GCE_METADATA_HOST", metadataHost)

	return srv
}

// checkMetadataFlavor adds middleware to check for the Google Metadata flavor header
// and handle redirects for missing trailing slashes
func checkMetadataFlavor(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check for required Metadata-Flavor header
		if r.Header.Get("Metadata-Flavor") != "Google" {
			w.WriteHeader(http.StatusForbidden)

			return
		}

		wrec := httptest.NewRecorder()
		handler.ServeHTTP(wrec, r)

		// try again on 301s - likely just a trailing `/` missing
		if wrec.Code == http.StatusMovedPermanently {
			if !strings.HasSuffix(r.URL.Path, "/") {
				r.URL.Path += "/"
			}

			handler.ServeHTTP(w, r)

			return
		}

		maps.Copy(w.Header(), wrec.Header())
		w.WriteHeader(wrec.Code)
		_, _ = w.Write(wrec.Body.Bytes())
	})
}

// handleMetadataRequest returns a handler for metadata requests that serves content
// from the mock filesystem
func handleMetadataRequest(metafsys fstest.MapFS) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Strip the prefix to get the actual path
		path := strings.TrimPrefix(r.URL.Path, "/computeMetadata/v1/")

		// Root path special handling
		if path == "" {
			_, _ = w.Write([]byte("instance/\nproject/\n"))

			return
		}

		// Special case for ReadDir tests - split long line for linter
		if path == "instance/" {
			dirContent := []string{
				"attributes/", "cpu-platform", "disks/", "hostname", "id", "image",
				"machine-type", "network-interfaces/", "service-accounts/", "zone",
			}
			_, _ = w.Write([]byte(strings.Join(dirContent, "\n") + "\n"))

			return
		}

		// Special cases for other directory paths
		if path == "project/" {
			_, _ = w.Write([]byte("attributes/\nnumeric-project-id\nproject-id\n"))

			return
		}

		if path == "instance/network-interfaces/" {
			_, _ = w.Write([]byte("0/\n"))

			return
		}

		if path == "instance/service-accounts/" {
			_, _ = w.Write([]byte("default/\n"))

			return
		}

		// Check if it's a directory request (ends with /)
		if strings.HasSuffix(path, "/") {
			entries := getDirectoryEntries(metafsys, path)
			if len(entries) == 0 {
				// Directory not found
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprintf(w, "GCE metadata %q not defined", path)

				return
			}

			_, _ = w.Write([]byte(strings.Join(entries, "\n")))

			return
		}

		// Check if the file exists
		if file, ok := metafsys[path]; ok {
			_, _ = w.Write(file.Data)

			return
		}

		// File not found - the Google client expects 404 responses for missing metadata
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, "GCE metadata %q not defined", path)
	}
}

// getDirectoryEntries extracts directory entries for a given path prefix
func getDirectoryEntries(fs fstest.MapFS, prefix string) []string {
	entries := map[string]struct{}{}

	// Look for files with this prefix in the mock filesystem
	for name := range fs {
		if !strings.HasPrefix(name, prefix) {
			continue
		}

		// Get the relative path component
		relPath := strings.TrimPrefix(name, prefix)
		if relPath == "" {
			continue
		}

		// Handle subdirectories and files
		entryName := relPath
		if idx := strings.Index(relPath, "/"); idx >= 0 {
			// This is a subdirectory entry - use only the first component with trailing slash
			entryName = relPath[:idx+1]
		}

		// Add to the set of unique entries
		entries[entryName] = struct{}{}
	}

	// Convert map to slice
	result := make([]string, 0, len(entries))
	for entry := range entries {
		result = append(result, entry)
	}

	return result
}

// createMockFS creates the fake metadata filesystem for testing
func createMockFS() fstest.MapFS {
	return fstest.MapFS{
		// Instance metadata values
		"instance/attributes/enable-oslogin": &fstest.MapFile{Data: []byte("FALSE")},
		"instance/attributes/shell":          &fstest.MapFile{Data: []byte("bash")},
		"instance/cpu-platform":              &fstest.MapFile{Data: []byte("Intel Haswell")},
		"instance/disks/0/device-name":       &fstest.MapFile{Data: []byte("persistent-disk-0")},
		"instance/disks/0/index":             &fstest.MapFile{Data: []byte("0")},
		"instance/disks/0/mode":              &fstest.MapFile{Data: []byte("READ_WRITE")},
		"instance/disks/0/type":              &fstest.MapFile{Data: []byte("PERSISTENT")},
		"instance/hostname":                  &fstest.MapFile{Data: []byte("instance-1.c.project-id.internal")},
		"instance/id":                        &fstest.MapFile{Data: []byte("1234567890123456789")},
		"instance/image": &fstest.MapFile{
			Data: []byte("projects/debian-cloud/global/images/debian-10-buster-v20220406"),
		},
		"instance/machine-type": &fstest.MapFile{
			Data: []byte("projects/123456789012/machineTypes/e2-medium"),
		},
		"instance/network-interfaces/0/ip":                           &fstest.MapFile{Data: []byte("10.0.0.2")},
		"instance/network-interfaces/0/mac":                          &fstest.MapFile{Data: []byte("42:01:0a:00:00:02")},
		"instance/network-interfaces/0/mtu":                          &fstest.MapFile{Data: []byte("1460")},
		"instance/network-interfaces/0/network":                      &fstest.MapFile{Data: []byte("projects/123456789012/networks/default")},
		"instance/network-interfaces/0/subnetmask":                   &fstest.MapFile{Data: []byte("255.255.255.0")},
		"instance/network-interfaces/0/access-configs/0/external-ip": &fstest.MapFile{Data: []byte("34.123.123.123")},
		"instance/network-interfaces/0/access-configs/0/type":        &fstest.MapFile{Data: []byte("ONE_TO_ONE_NAT")},
		"instance/service-accounts/default/aliases":                  &fstest.MapFile{Data: []byte("default")},
		"instance/service-accounts/default/email": &fstest.MapFile{
			Data: []byte("123456789012-compute@developer.gserviceaccount.com"),
		},
		"instance/service-accounts/default/identity": &fstest.MapFile{
			Data: []byte(`{"audience":"http://example.com","format":"full","licenses":` +
				`["https://www.googleapis.com/compute/v1/projects/debian-cloud/global/licenses/debian-10-buster"]}`),
		},
		"instance/service-accounts/default/scopes": &fstest.MapFile{
			Data: []byte(
				"https://www.googleapis.com/auth/devstorage.read_only\n" +
					"https://www.googleapis.com/auth/logging.write\n" +
					"https://www.googleapis.com/auth/monitoring.write\n" +
					"https://www.googleapis.com/auth/servicecontrol\n" +
					"https://www.googleapis.com/auth/service.management.readonly\n" +
					"https://www.googleapis.com/auth/trace.append"),
		},
		"instance/service-accounts/default/token": &fstest.MapFile{
			Data: []byte(`{"access_token":"ya29.TEST_TOKEN","expires_in":3599,"token_type":"Bearer"}`),
		},
		"instance/zone": &fstest.MapFile{Data: []byte("projects/123456789012/zones/us-central1-a")},

		// Project metadata values
		"project/attributes/ssh-keys": &fstest.MapFile{
			Data: []byte("user:ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQC/JxGByvHDHgQAU+0nRFWdvMPi22OgNUn9ansrI8QN1ZJGxD1ML8DRnJ3Q3zFK" +
				"qqjGucfNWW0xpVib+ttkIBp8G9P/EOcX9C3FF63O3SnnIUHJsp5faRAZsTJPx0G5HUbvhBvnAcCtSqQgmr02c1l582vAWx48pOmeXXMkl9qe9V/s7K3" +
				"utmeZkRLo9DqnbsDlg5GWxLC/rWKYaZR66CnMEyZ7yBy3v3abKaGGRovLkHNAgWjSSgmUTI1nT5/S2OLxxuDnsC7+BiABLPaqlIE70SzcWZ0swx68Bo" +
				"2AY9T9ymGqeAM/1T4yRtg0sPB98TpT7WrY5A3iia2UVtLO/xcTt test"),
		},
		"project/numeric-project-id": &fstest.MapFile{Data: []byte("123456789012")},
		"project/project-id":         &fstest.MapFile{Data: []byte("test-project-id")},
	}
}
