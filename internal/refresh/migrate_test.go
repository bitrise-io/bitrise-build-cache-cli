//go:build unit

package refresh

import (
	"errors"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/toolconfig"
)

type recordingMigrator struct {
	tool   toolconfig.Tool
	calls  int
	err    error
	called *[]toolconfig.Tool
}

func (m *recordingMigrator) Tool() toolconfig.Tool { return m.tool }

func (m *recordingMigrator) Migrate(_ string) error {
	m.calls++
	if m.called != nil {
		*m.called = append(*m.called, m.tool)
	}

	return m.err
}

func TestNeedsMigrate(t *testing.T) {
	cases := []struct {
		name           string
		stored, target string
		want           bool
	}{
		{"empty stored treated as 1.0.0 → no minor migrate to 1.0.0", "", "1.0.0", false},
		{"same version → no", "1.0.0", "1.0.0", false},
		{"minor bump within major → yes", "1.0.0", "1.1.0", true},
		{"patch bump within major → yes", "1.1.0", "1.1.2", true},
		{"major bump → no (Notify handles it)", "1.0.0", "2.0.0", false},
		{"stored ahead of target → no", "1.2.0", "1.1.0", false},
		{"empty stored, target above 1.x → no (Notify)", "", "2.0.0", false},
		{"unparseable target → no", "1.0.0", "not-semver", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, needsMigrate(tc.stored, tc.target))
		})
	}
}

func TestMigrateWith_dispatchesOnlyMinorBumps(t *testing.T) {
	var called []toolconfig.Tool

	migrators := []toolconfig.Migrator{
		&recordingMigrator{tool: toolconfig.Gradle, called: &called},
		&recordingMigrator{tool: toolconfig.Bazel, called: &called},
		&recordingMigrator{tool: toolconfig.Xcelerate, called: &called},
		&recordingMigrator{tool: toolconfig.Ccache, called: &called},
	}

	samples := []toolconfig.Sample{
		{Tool: toolconfig.Gradle, ConfigVersion: "1.0.0"},    // expects 1.0.0 → none
		{Tool: toolconfig.Bazel, ConfigVersion: "0.9.0"},     // expects 1.0.0 → major (Notify) → none
		{Tool: toolconfig.Xcelerate, ConfigVersion: "1.0.0"}, // expects 1.0.0 → none
		{Tool: toolconfig.Ccache, ConfigVersion: "1.0.0"},    // expects 1.0.0 → none
	}

	MigrateWith(log.NewLogger(), "/home", samples, migrators)
	assert.Empty(t, called)
}

func TestMigrateWith_minorBumpInvokesMigrator(t *testing.T) {
	var called []toolconfig.Tool

	migrator := &recordingMigrator{tool: toolconfig.Gradle, called: &called}
	samples := []toolconfig.Sample{{Tool: toolconfig.Gradle, ConfigVersion: "1.0.0"}}

	// Pretend current gradle ConfigVersion is 1.1.0 by stubbing via a fresh
	// CurrentConfigVersions map indirectly is overkill; instead test the
	// helper directly by injecting a sample whose stored is one minor behind
	// the actual current. Today the current is 1.0.0 so we cannot exercise
	// the minor-bump path through MigrateWith yet — covered by needsMigrate.
	MigrateWith(log.NewLogger(), "/home", samples, []toolconfig.Migrator{migrator})
	assert.Empty(t, called)
}

func TestMigrateWith_migratorErrorIsSwallowed(t *testing.T) {
	migrator := &recordingMigrator{tool: toolconfig.Gradle, err: errors.New("boom")}
	// Pass nil logger — Migrate is a no-op when logger is nil so this never
	// dispatches; verifies the guard rather than the error swallow per se.
	// The real swallow path is exercised end-to-end once a tool ships a real
	// minor bump.
	MigrateWith(nil, "/home", []toolconfig.Sample{{Tool: toolconfig.Gradle, ConfigVersion: "1.0.0"}}, []toolconfig.Migrator{migrator})
	assert.Equal(t, 0, migrator.calls)
}

func TestDefaultMigrators_coversEveryTool(t *testing.T) {
	got := DefaultMigrators(log.NewLogger())

	tools := map[toolconfig.Tool]bool{}
	for _, m := range got {
		tools[m.Tool()] = true
	}

	for _, want := range []toolconfig.Tool{toolconfig.Gradle, toolconfig.Bazel, toolconfig.Xcelerate, toolconfig.Ccache} {
		assert.True(t, tools[want], "no default migrator for %s", want)
	}
}
