//go:build unit

package doctor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/spawn"
)

// `doctor --fix` is a service entry point like the wrapper is, so it has to
// start the same thing. Carrying its own argv would let it drift and start a
// service the build never talks to.
func TestFixers_CarryTheServicesSpawnDefines(t *testing.T) {
	assert.Equal(t, spawn.XcelerateProxy(), StartProxyFixer().Service)
	assert.Equal(t, spawn.CcacheHelper(), StartCcacheHelperFixer().Service)
}

func TestStartServiceFixer_SpawnsTheServiceItCarries(t *testing.T) {
	for _, f := range []StartServiceFixer{StartProxyFixer(), StartCcacheHelperFixer()} {
		t.Run(f.Label, func(t *testing.T) {
			var got spawn.Service
			f.Start = func(svc spawn.Service) (int, error) {
				got = svc

				return 99, nil
			}

			msg, err := f.Fix()
			require.NoError(t, err)

			assert.Equal(t, f.Service, got)
			assert.Contains(t, msg, f.Label)
		})
	}
}
