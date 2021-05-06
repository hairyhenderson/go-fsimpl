package autofs

import (
	"fmt"
	"io/fs"
)

func ExampleLookup() {
	fsys, _ := Lookup("file:///somedir")

	list, _ := fs.ReadDir(fsys, ".")

	for _, entry := range list {
		fmt.Printf("found %s\n", entry.Name())
	}

	// Output:
	//
}
