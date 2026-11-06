//go:build unit

package enrichment_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/enrichment"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/sessions"
)

func TestClassify_RulesetTable(t *testing.T) {
	cases := []struct {
		action string
		qos    string
		want   enrichment.RowType
	}{
		{"build", "", enrichment.RowMainBuild},
		{"build", "default", enrichment.RowDarkBuild},
		{"indexbuild", "prepareForIndexing", enrichment.RowIndexer},
		{"resolvePackages", "", enrichment.RowPackageResolve},
		{"resolve", "", enrichment.RowPackageResolve},
		{"cleanBuildFolder", "", enrichment.RowClean},
		{"clean", "", enrichment.RowClean},
		{"archive", "", enrichment.RowArchive},
		{"test", "", enrichment.RowTest},
		{"installHeaders", "", enrichment.RowInstallHeaders},
		{"", "", enrichment.RowUnclassified},
		{"weirdAction", "someQoS", enrichment.RowUnclassified},
	}

	for _, c := range cases {
		t.Run(c.action+"/"+c.qos, func(t *testing.T) {
			assert.Equal(t, c.want, enrichment.Classify(c.action, c.qos))
		})
	}
}

func TestGroupKey_AnchoredSidecar(t *testing.T) {
	s := sessions.Sidecar{
		WrapperSession: &sessions.WrapperLink{InvocationID: "inv-abc"},
	}
	assert.Equal(t, "inv-abc", enrichment.GroupKey(s))

	anchorless := sessions.Sidecar{}
	assert.Equal(t, "", enrichment.GroupKey(anchorless))
}
