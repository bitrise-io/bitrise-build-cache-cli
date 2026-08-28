package bazelconfig

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

func Test_WorkflowNameHeaderValue(t *testing.T) {
	tests := []struct {
		name         string
		workflowName string
		want         string
	}{
		{
			name: "empty stays empty",
			want: "",
		},
		{
			name:         "plain name is left alone",
			workflowName: "primary",
			want:         "primary",
		},
		{
			name:         "space is left intact for the quoted value",
			workflowName: "Run UI tests",
			want:         "Run UI tests",
		},
		{
			name:         "apostrophe is escaped so it does not close the quote",
			workflowName: "Pat's workflow",
			want:         `Pat\'s workflow`,
		},
		{
			name:         "backslash is escaped so the separator survives",
			workflowName: `Deploy \ Release`,
			want:         `Deploy \\ Release`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inventory := CommonTemplateInventory{WorkflowName: tt.workflowName}

			assert.Equal(t, tt.want, inventory.WorkflowNameHeaderValue())
		})
	}
}

// The header is emitted on both the cache and the BES side, so both have to
// carry the escaped value.
func Test_Generate_WorkflowNameIsEscaped(t *testing.T) {
	inventory := TemplateInventory{
		Common: CommonTemplateInventory{
			CLIPath:      "/usr/local/bin/bitrise-build-cache",
			WorkflowName: "Pat's workflow",
		},
		Cache: CacheTemplateInventory{
			Enabled:             true,
			EndpointURLWithPort: "grpcs://cache.services.bitrise.io:443",
			IsPushEnabled:       true,
		},
		BES: BESTemplateInventory{
			Enabled:             true,
			EndpointURLWithPort: "grpcs://flare-bes.services.bitrise.io:443",
		},
	}

	got, err := inventory.GenerateBazelrc(utils.DefaultTemplateProxy())

	require.NoError(t, err)
	assert.Contains(t, got, `build --remote_header='x-workflow-name=Pat\'s workflow'`+"\n")
	assert.Contains(t, got, `build --bes_header='x-workflow-name=Pat\'s workflow'`+"\n")
}
