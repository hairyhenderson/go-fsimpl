package sopsfs

import (
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestExample(t *testing.T) {
	// Get current working directory
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current working directory: %v", err)
	}

	// Construct absolute path to the fixture file
	fixture, _ := filepath.Abs(fmt.Sprintf("%s/../internal/tests/integration/sops/.sops-integration-test.enc.yaml", wd))

	base, _ := url.Parse("sops://?format=yaml")

	fsys, _ := New(base)

	b, err := fs.ReadFile(fsys, fixture)
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
		t.Error(fmt.Errorf("Expected SOPS file to match default value, got:\n\"%s\"\n expected:\n\"%s\"", string(b), yaml))
	}
}

func TestJSON(t *testing.T) {
	// Get current working directory
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current working directory: %v", err)
	}

	// Construct absolute path to the fixture file
	fixture, _ := filepath.Abs(fmt.Sprintf("%s/../internal/tests/integration/sops/.sops-integration-test.enc.json", wd))

	base, _ := url.Parse("sops://?format=json")

	fsys, _ := New(base)

	b, err := fs.ReadFile(fsys, fixture)
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
		t.Error(fmt.Errorf("Expected SOPS file to match default value, got:\n\"%s\"\n expected:\n\"%s\"", string(b), json))
	}
}
