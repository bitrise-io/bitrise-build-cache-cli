//go:build unit

package permhint

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
)

func loggerWithBuffer(buf *bytes.Buffer) log.Logger {
	return log.NewLogger(log.WithOutput(buf))
}

func TestPrintIfApplicable_permissionErrorPrintsRemediation(t *testing.T) {
	pathErr := &fs.PathError{
		Op:   "mkdir",
		Path: "/Users/alice/.local/state/bitrise-build-cache",
		Err:  fs.ErrPermission,
	}
	wrapped := fmt.Errorf("create log dir: %w", pathErr)

	var buf bytes.Buffer
	PrintIfApplicable(loggerWithBuffer(&buf), wrapped)

	out := buf.String()
	assert.NotEmpty(t, out)
	assert.Contains(t, out, "/Users/alice/.local/state/bitrise-build-cache")
	assert.Contains(t, out, "sudo chown")
	assert.Contains(t, out, "~/.local/state")
}

func TestPrintIfApplicable_nonPermissionErrorIsNoop(t *testing.T) {
	var buf bytes.Buffer
	PrintIfApplicable(loggerWithBuffer(&buf), errors.New("something else went wrong"))
	assert.Empty(t, buf.String())
}

func TestPrintIfApplicable_nilErrorIsNoop(t *testing.T) {
	var buf bytes.Buffer
	PrintIfApplicable(loggerWithBuffer(&buf), nil)
	assert.Empty(t, buf.String())
}

func TestIsPermissionError_truthTable(t *testing.T) {
	assert.False(t, isPermissionError(nil))
	assert.False(t, isPermissionError(errors.New("plain error")))
	assert.True(t, isPermissionError(fs.ErrPermission))
	assert.True(t, isPermissionError(&fs.PathError{Op: "mkdir", Path: "/x", Err: fs.ErrPermission}))
	assert.True(t, isPermissionError(fmt.Errorf("wrap: %w", fs.ErrPermission)))
}

func TestPathErrorPath_extractsFromWrap(t *testing.T) {
	pathErr := &fs.PathError{Op: "mkdir", Path: "/foo/bar", Err: fs.ErrPermission}
	wrapped := fmt.Errorf("outer: %w", pathErr)
	assert.Equal(t, "/foo/bar", pathErrorPath(wrapped))
}

func TestPathErrorPath_returnsEmptyForNonPathError(t *testing.T) {
	assert.Equal(t, "", pathErrorPath(errors.New("nope")))
}

func TestOwnerOfNearestAncestor_returnsExistingAncestor(t *testing.T) {
	missing := "/tmp/this-directory-should-not-exist-bitrise-test-" + strings.Repeat("x", 16)
	parent, _, ok := ownerOfNearestAncestor(missing)
	assert.True(t, ok)
	assert.True(t, strings.HasPrefix(missing, parent) || parent == "/tmp" || parent == "/",
		"ancestor %q should be a prefix of %q (or root)", parent, missing)
}
