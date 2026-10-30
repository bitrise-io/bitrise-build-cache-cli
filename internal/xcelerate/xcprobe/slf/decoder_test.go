//go:build unit

package slf_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/xcelerate/xcprobe/slf"
)

func TestDecode_RealFixture(t *testing.T) {
	path := filepath.Join("testdata", "sample.xcactivitylog")

	f, err := os.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })

	root, version, err := slf.Decode(f)
	require.NoError(t, err)
	require.NotNil(t, root)

	// Fixture is Xcode 26.x; current XCLogParser doc tops out at v12, our
	// fixture is v13. The decoder is version-tolerant on unknown-tail probes.
	assert.GreaterOrEqual(t, int(version), 11, "fixture SLF version floor is v11 for attachment coverage")

	assert.Equal(t, "IDEActivityLogSection", root.ClassName)
	assert.Equal(t, "Build CasProbe", root.Title)
	assert.InDelta(t, 5.663, root.Duration(), 0.5)
	assert.NotEmpty(t, root.SubSteps, "root must have child sections (Prepare build + Build target …)")

	// Walk tree, assert the invariants the spec calls out.
	steps, attachments, targetNames, bomAttachments := walkTree(root)

	assert.Greater(t, steps, 100, "fixture should expand to >100 sections")
	assert.Greater(t, attachments, 0, "fixture is SLF v11+, must carry attachments")
	assert.Contains(t, targetNames, "Build target CasProbe", "target name must be recovered from a section title")
	assert.Greater(t, bomAttachments, 0, "fixture must contain at least one BuildOperationMetrics attachment")
}

func TestDecode_RealFixture_CompilationCacheMetrics(t *testing.T) {
	path := filepath.Join("testdata", "sample.xcactivitylog")

	f, err := os.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })

	root, _, err := slf.Decode(f)
	require.NoError(t, err)

	ccm := aggregateCCM(root)
	// Fixture carries 65 Swift cache hits as reported by the pre-existing
	// regex reader; the attachment path recovers the same number from
	// BuildOperationMetrics (Xcode 26.x emits it as "swiftCacheHits").
	assert.Equal(t, int64(65), ccm.SwiftCacheHits, "swift cache hits must round-trip through BuildOperationMetrics")
	assert.Equal(t, int64(0), ccm.SwiftCacheMisses, "fixture is a cache-saturated build")
	assert.Positive(t, ccm.Attachments, "aggregator must record attachment count")
}

// walkTree is a small helper so each test can assert independent slices of
// the fixture without the walker having to be its own exported helper.
func walkTree(root *slf.BuildStep) (steps, attachments int, targetNames map[string]bool, bomAttachments int) {
	targetNames = map[string]bool{}

	var walk func(b *slf.BuildStep)
	walk = func(b *slf.BuildStep) {
		steps++

		if b.IsTargetStep() {
			targetNames[b.Title] = true
		}

		for _, a := range b.Attachments {
			attachments++

			if a.BuildOp != nil {
				bomAttachments++
			}
		}

		for _, s := range b.SubSteps {
			walk(s)
		}
	}

	walk(root)

	return steps, attachments, targetNames, bomAttachments
}

func aggregateCCM(root *slf.BuildStep) slf.CompilationCacheMetrics {
	var ccm slf.CompilationCacheMetrics

	var walk func(b *slf.BuildStep)
	walk = func(b *slf.BuildStep) {
		for _, a := range b.Attachments {
			if a.BuildOp == nil {
				continue
			}

			ccm.Attachments++

			for k, v := range a.BuildOp.Counters {
				switch k {
				case "clangCacheHits":
					ccm.ClangCacheHits += v
				case "clangCacheMisses":
					ccm.ClangCacheMisses += v
				case "swiftCacheHits":
					ccm.SwiftCacheHits += v
				case "swiftCacheMisses":
					ccm.SwiftCacheMisses += v
				}
			}
		}

		for _, s := range b.SubSteps {
			walk(s)
		}
	}

	walk(root)

	return ccm
}
