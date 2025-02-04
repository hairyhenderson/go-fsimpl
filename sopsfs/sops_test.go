package sopsfs

import (
	"fmt"
	"github.com/getsops/sops/v3/pgp"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

const (
	YAML_FIXTURE_FILE    = ".sops-integration-test.enc.yaml"
	YAML_FIXTURE_CONTENT = `hello: Welcome to SOPS! Edit this file as you please!
example_key: example_value
# Example comment!
example_array:
    - example_value1
    - example_value2
example_number: 1234.56789
example_booleans:
    - true
    - false
`
	JSON_FIXTURE_FILE    = ".sops-integration-test.enc.json"
	JSON_FIXTURE_CONTENT = `{
	"hello": "Welcome to SOPS! Edit this file as you please!",
	"example_key": "example_value",
	"example_array": [
		"example_value1",
		"example_value2"
	],
	"example_number": 1234.56789,
	"example_booleans": [
		true,
		false
	]
}`
)

// Returns path to SOPS fixtures and initializes GPG with encryption key imported
func testSetup(t *testing.T) (string, pgp.GnuPGHome) {
	wd, _ := os.Getwd()

	sopsFixturesDir, _ := filepath.Abs(fmt.Sprintf("%s/../internal/tests/integration/sops/", wd))
	sopsFixturesDir += "/"

	home, err := pgp.NewGnuPGHome()
	if err != nil {
		t.Fatal(err)
	}
	os.Setenv("GNUPGHOME", home.String())

	if err = home.ImportFile(fmt.Sprintf("%s/sops_functional_tests_key.asc", sopsFixturesDir)); err != nil {
		t.Error(err)
	}
	return sopsFixturesDir, home
}

func TestYAMLandJSON(t *testing.T) {
	sopsPath, home := testSetup(t)
	defer home.Cleanup()

	fsys, _ := New(&url.URL{Path: sopsPath})

	b, err := fs.ReadFile(fsys, YAML_FIXTURE_FILE)

	if err != nil {
		t.Error(err)
	}

	if string(b) != YAML_FIXTURE_CONTENT {
		t.Error(fmt.Errorf("expected SOPS YAML to match default value, got: %s expected: %s",
			string(b),
			YAML_FIXTURE_CONTENT,
		))
	}

	b, err = fs.ReadFile(fsys, JSON_FIXTURE_FILE)

	if err != nil {
		t.Error(err)
	}

	if string(b) != JSON_FIXTURE_CONTENT {
		t.Error(fmt.Errorf("expected SOPS JSON to match default value, got: %s expected: %s",
			string(b),
			JSON_FIXTURE_CONTENT,
		))
	}
}

func TestJSONFile(t *testing.T) {
	sopsPath, home := testSetup(t)
	defer home.Cleanup()

	fsys, _ := New(&url.URL{Path: filepath.Join(sopsPath, JSON_FIXTURE_FILE)})

	b, err := fs.ReadFile(fsys, JSON_FIXTURE_FILE)

	if err != nil {
		t.Error(err)
	}

	if string(b) != JSON_FIXTURE_CONTENT {
		t.Error(fmt.Errorf("expected SOPS JSON to match default value, got: %s expected: %s",
			string(b),
			JSON_FIXTURE_CONTENT,
		))
	}
}

func TestYAMLFile(t *testing.T) {
	sopsPath, home := testSetup(t)
	defer home.Cleanup()

	fsys, _ := New(&url.URL{Path: filepath.Join(sopsPath, YAML_FIXTURE_FILE)})

	b, err := fs.ReadFile(fsys, YAML_FIXTURE_FILE)

	if err != nil {
		t.Error(err)
	}

	if string(b) != YAML_FIXTURE_CONTENT {
		t.Error(fmt.Errorf("expected SOPS YAML to match default value, got: %s expected: %s",
			string(b),
			YAML_FIXTURE_CONTENT,
		))
	}
}
