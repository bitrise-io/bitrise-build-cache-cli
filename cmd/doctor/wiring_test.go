//go:build unit

package doctor

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

// TestDoctorCmd_WiresInteractivePrompts guards against regressing the three
// interactive prompt wirings on Doctor. A nil ProjectScopePrompt makes the
// opt-in fixer dead-on-arrival (falls into the "no confirm prompt wired"
// branch), so each prompt must have an assignment in RunE.
func TestDoctorCmd_WiresInteractivePrompts(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	doctorFile := filepath.Join(filepath.Dir(thisFile), "doctor.go")

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, doctorFile, nil, parser.SkipObjectResolution)
	require.NoError(t, err)

	wanted := map[string]bool{
		"AuthFixPrompt":       false,
		"WorkspacePickPrompt": false,
		"ProjectScopePrompt":  false,
	}

	ast.Inspect(file, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != 1 {
			return true
		}
		sel, ok := assign.Lhs[0].(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if _, tracked := wanted[sel.Sel.Name]; tracked {
			wanted[sel.Sel.Name] = true
		}

		return true
	})

	for name, found := range wanted {
		assert.True(t, found, "doctor.go must assign d.%s so the fixer isn't dead-on-arrival", name)
	}
}
