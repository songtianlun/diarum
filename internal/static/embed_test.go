package static

import (
	"io/fs"
	"testing"
)

func TestGetFS(t *testing.T) {
	fsys, err := GetFS()
	if err != nil {
		t.Fatalf("GetFS: %v", err)
	}

	if _, err := fs.Stat(fsys, "index.html"); err != nil {
		t.Fatalf("embedded index.html should exist: %v", err)
	}
}
