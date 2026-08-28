package bazelconfig

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/utils"
)

func Test_BuildUserHeaderValue(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		username string
		want     string
	}{
		{
			name:     "CIProvider wins over the resolved username",
			provider: "bitrise",
			username: "jane.doe",
			want:     "bitrise",
		},
		{
			name:     "falls back to the resolved username",
			username: "jane.doe",
			want:     "jane.doe",
		},
		{
			name: "no CIProvider and no username is empty",
			want: "",
		},
		{
			name:     "display name with a space is left intact for the quoted value",
			username: "Jane Doe",
			want:     "Jane Doe",
		},
		{
			name:     "apostrophe is escaped so it does not close the quote",
			username: "Pat O'Brien",
			want:     `Pat O\'Brien`,
		},
		{
			name:     "backslash is escaped so a domain user keeps its separator",
			username: `CORP\jdoe`,
			want:     `CORP\\jdoe`,
		},
		{
			name:     "backslash is escaped before the apostrophe it precedes",
			username: `CORP\O'Brien`,
			want:     `CORP\\O\'Brien`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inventory := CommonTemplateInventory{
				CIProvider:   tt.provider,
				HostMetadata: HostMetadataInventory{Username: tt.username},
			}

			assert.Equal(t, tt.want, inventory.BuildUserHeaderValue())
		})
	}
}

// A display name with a space used to leave a bare `Doe` token at the end of the
// rc line, which Bazel read as a target pattern — failing every command with
// `no such target '//:Doe'`.
func Test_Generate_BuildUserWithSpaceIsQuoted(t *testing.T) {
	inventory := TemplateInventory{
		Common: CommonTemplateInventory{
			CLIPath:      "/usr/local/bin/bitrise-build-cache",
			HostMetadata: HostMetadataInventory{Username: "Jane Doe"},
		},
		Cache: CacheTemplateInventory{
			Enabled:             true,
			EndpointURLWithPort: "grpcs://cache.services.bitrise.io:443",
			IsPushEnabled:       true,
		},
	}

	got, err := inventory.GenerateBazelrc(utils.DefaultTemplateProxy())

	require.NoError(t, err)
	assert.Contains(t, got, "build --remote_header='x-flare-builduser=Jane Doe'\n")
}
