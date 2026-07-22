//go:build unit

package enrichment_test

import "os"

func openAppend(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
}
