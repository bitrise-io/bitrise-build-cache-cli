//go:build unit

package xcelerate

import (
	"io/fs"
	"os"
)

// anchorMissing is a package-level Lstat stub reporting the anchor as absent.
// Shared across tests that exercise the "developer-dir derived" fallback path.
var anchorMissing = func(name string) (os.FileInfo, error) {
	return nil, &fs.PathError{Op: "lstat", Path: name, Err: fs.ErrNotExist}
}

// anchorPresent is a package-level Lstat stub reporting the anchor as present.
// Shared across tests that exercise the "anchor-rooted" branch.
var anchorPresent = func(_ string) (os.FileInfo, error) {
	return nil, nil
}
