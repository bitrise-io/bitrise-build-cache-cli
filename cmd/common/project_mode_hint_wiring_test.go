//go:build unit

package common_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPrintOptInGateHintIfGated_WiredIntoActivateWrappers checks that each
// activate wrapper calls PrintOptInGateHintIfGated exactly once, and that the
// call sits after PersistProjectMode in the function body so the hint reads
// the mode the wrapper just stored. The behaviour of the helper itself is
// covered by TestPrintOptInGateHintIfGated — this guards the wiring only.
func TestPrintOptInGateHintIfGated_WiredIntoActivateWrappers(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok, "runtime.Caller must succeed to resolve cmd/ root")
	cmdDir := filepath.Dir(filepath.Dir(thisFile))

	wrappers := []string{
		"xcode/activate_xcode.go",
		"gradle/activate_gradle.go",
		"ccache/activate_cpp.go",
		"bazel/activate_bazel.go",
		"reactnative/activate_react_native.go",
	}

	for _, rel := range wrappers {
		t.Run(rel, func(t *testing.T) {
			persistLine, hintLine, hintCalls := scanWrapperCalls(t, filepath.Join(cmdDir, rel))

			assert.Equal(t, 1, hintCalls,
				"wrapper %s must call PrintOptInGateHintIfGated exactly once; found %d", rel, hintCalls)
			require.NotZero(t, persistLine, "wrapper %s must call PersistProjectMode", rel)
			require.NotZero(t, hintLine, "wrapper %s must call PrintOptInGateHintIfGated", rel)
			assert.Greater(t, hintLine, persistLine,
				"wrapper %s must call PrintOptInGateHintIfGated AFTER PersistProjectMode (persist line=%d, hint line=%d)",
				rel, persistLine, hintLine)
		})
	}
}

// scanWrapperCalls returns the source-line positions of the first
// PersistProjectMode call and the first PrintOptInGateHintIfGated call in the
// file, plus the total count of hint calls. The wiring test uses source order
// as a proxy for execution order: both calls sit as top-level statements in
// each wrapper, so AST line order is sound.
func scanWrapperCalls(t *testing.T, path string) (persistLine, hintLine, hintCalls int) {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	require.NoError(t, err)

	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch calleeName(call.Fun) {
		case "PersistProjectMode":
			if persistLine == 0 {
				persistLine = fset.Position(call.Pos()).Line
			}
		case "PrintOptInGateHintIfGated":
			if hintLine == 0 {
				hintLine = fset.Position(call.Pos()).Line
			}
			hintCalls++
		}

		return true
	})

	return persistLine, hintLine, hintCalls
}

// calleeName returns the final identifier of a call expression — e.g.
// `common.PrintOptInGateHintIfGated` yields "PrintOptInGateHintIfGated".
func calleeName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.SelectorExpr:
		return f.Sel.Name
	case *ast.Ident:
		return f.Name
	}

	return ""
}
