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

func TestYAMLandJSON(t *testing.T) {
	// Get current working directory
	wd, _ := os.Getwd()
	sopsPath, _ := filepath.Abs(fmt.Sprintf("%s/../internal/tests/integration/sops/", wd))
	sopsPath += "/"

	home, err := pgp.NewGnuPGHome()
	if err != nil {
		t.Error(err)
	}
	os.Setenv("GNUPGHOME", home.String())

	if err = home.ImportFile(fmt.Sprintf("%s/sops_functional_tests_key.asc", sopsPath)); err != nil {
		t.Error(err)
	}
	defer home.Cleanup()

	base, _ := url.Parse(fmt.Sprintf("sops://%s", sopsPath))

	fsys, _ := New(base)

	b, err := fs.ReadFile(fsys, ".sops-integration-test.enc.yaml")
	yaml := `hello: Welcome to SOPS! Edit this file as you please!
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
	if string(b) != yaml {
		t.Error(fmt.Errorf("Expected SOPS YAML to match default value, got:\n\"%s\"\n expected:\n\"%s\"", string(b), yaml))
	}

	if err != nil {
		t.Error(err)
	}

	b, err = fs.ReadFile(fsys, ".sops-integration-test.enc.json")
	json := `{
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
	if string(b) != json {
		t.Error(fmt.Errorf("Expected SOPS JSON to match default value, got:\n\"%s\"\n expected:\n\"%s\"", string(b), json))
	}
}
