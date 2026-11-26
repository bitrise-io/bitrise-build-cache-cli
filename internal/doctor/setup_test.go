//go:build unit

package doctor

import (
	"testing"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/testhome"
)

func TestMain(m *testing.M) {
	testhome.Main(m)
}
