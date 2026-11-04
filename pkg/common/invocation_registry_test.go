//go:build unit

package common

import (
	"context"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/analytics/multiplatform"
	"github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/auth"
	ccacheanalytics "github.com/bitrise-io/bitrise-build-cache-cli/v3/internal/ccache/analytics"
)

type stubInvocationsAPI struct {
	capturedInvocation multiplatform.Invocation
	capturedRelation   multiplatform.InvocationRelation
	invocationErr      error
	relationErr        error
}

func (s *stubInvocationsAPI) PutInvocation(inv multiplatform.Invocation) error {
	s.capturedInvocation = inv

	return s.invocationErr
}

func (s *stubInvocationsAPI) PutInvocationRelation(rel multiplatform.InvocationRelation) error {
	s.capturedRelation = rel

	return s.relationErr
}

func newTestRegistry(envs map[string]string) *InvocationRegistry {
	return &InvocationRegistry{
		resolve: func(context.Context) (auth.Credential, auth.Origin, error) {
			return auth.Credential{Token: "test-token", WorkspaceID: "test-workspace"},
				auth.Origin{Backend: auth.BackendFile, Provenance: auth.ProvenanceStatic}, nil
		},
		params: InvocationRegistryParams{
			Envs: envs,
		},
		logger: log.NewLogger(),
	}
}

func TestInvocationRegistry_RegisterMultiplatformInvocation(t *testing.T) {
	t.Run("sends invocation with provided BuildTool", func(t *testing.T) {
		stub := &stubInvocationsAPI{}
		reg := newTestRegistry(map[string]string{})
		reg.api = stub

		err := reg.RegisterMultiplatformInvocation(context.Background(), RegisterInvocationParams{
			InvocationID: "inv-123",
			BuildTool:    "gradle",
		})

		require.NoError(t, err)
		assert.Equal(t, "inv-123", stub.capturedInvocation.InvocationID)
		assert.Equal(t, "gradle", stub.capturedInvocation.BuildTool)
	})

	t.Run("defaults BuildTool to multiplatform", func(t *testing.T) {
		stub := &stubInvocationsAPI{}
		reg := newTestRegistry(map[string]string{})
		reg.api = stub

		err := reg.RegisterMultiplatformInvocation(context.Background(), RegisterInvocationParams{
			InvocationID: "inv-456",
		})

		require.NoError(t, err)
		assert.Equal(t, "multiplatform", stub.capturedInvocation.BuildTool)
	})

	t.Run("propagates PutInvocation error", func(t *testing.T) {
		stub := &stubInvocationsAPI{invocationErr: assert.AnError}
		reg := newTestRegistry(map[string]string{})
		reg.api = stub

		err := reg.RegisterMultiplatformInvocation(context.Background(), RegisterInvocationParams{
			InvocationID: "inv-789",
		})

		assert.ErrorContains(t, err, "register invocation")
	})

	t.Run("sets workspace ID from config auth", func(t *testing.T) {
		stub := &stubInvocationsAPI{}
		reg := newTestRegistry(map[string]string{})
		reg.api = stub

		err := reg.RegisterMultiplatformInvocation(context.Background(), RegisterInvocationParams{
			InvocationID: "inv-ws",
		})

		require.NoError(t, err)
		assert.Equal(t, "test-workspace", stub.capturedInvocation.BitriseWorkspaceSlug)
	})
}

func TestInvocationRegistry_RegisterRelation(t *testing.T) {
	t.Run("sends relation with provided BuildTool", func(t *testing.T) {
		stub := &stubInvocationsAPI{}
		reg := newTestRegistry(map[string]string{})
		reg.api = stub

		err := reg.RegisterRelation(context.Background(), RegisterRelationParams{
			ParentID:  "parent-1",
			ChildID:   "child-1",
			BuildTool: "gradle",
		})

		require.NoError(t, err)
		assert.Equal(t, "parent-1", stub.capturedRelation.ParentInvocationID)
		assert.Equal(t, "child-1", stub.capturedRelation.ChildInvocationID)
		assert.Equal(t, "gradle", stub.capturedRelation.BuildTool)
	})

	t.Run("defaults BuildTool to ccache", func(t *testing.T) {
		stub := &stubInvocationsAPI{}
		reg := newTestRegistry(map[string]string{})
		reg.api = stub

		err := reg.RegisterRelation(context.Background(), RegisterRelationParams{
			ParentID: "parent-2",
			ChildID:  "child-2",
		})

		require.NoError(t, err)
		assert.Equal(t, "ccache", stub.capturedRelation.BuildTool)
	})

	t.Run("propagates PutInvocationRelation error", func(t *testing.T) {
		stub := &stubInvocationsAPI{relationErr: assert.AnError}
		reg := newTestRegistry(map[string]string{})
		reg.api = stub

		err := reg.RegisterRelation(context.Background(), RegisterRelationParams{
			ParentID: "parent-3",
			ChildID:  "child-3",
		})

		assert.ErrorContains(t, err, "register invocation relation")
	})

	t.Run("sets InvocationDate", func(t *testing.T) {
		stub := &stubInvocationsAPI{}
		reg := newTestRegistry(map[string]string{})
		reg.api = stub

		err := reg.RegisterRelation(context.Background(), RegisterRelationParams{
			ParentID: "parent-4",
			ChildID:  "child-4",
		})

		require.NoError(t, err)
		assert.False(t, stub.capturedRelation.InvocationDate.IsZero())
	})
}

func TestInvocationRegistry_ResolveFailure(t *testing.T) {
	stub := &stubInvocationsAPI{}
	reg := newTestRegistry(map[string]string{})
	reg.api = stub
	reg.resolve = func(context.Context) (auth.Credential, auth.Origin, error) {
		return auth.Credential{}, auth.Origin{}, assert.AnError
	}

	err := reg.RegisterMultiplatformInvocation(context.Background(), RegisterInvocationParams{InvocationID: "inv"})
	require.ErrorIs(t, err, assert.AnError)

	err = reg.RegisterRelation(context.Background(), RegisterRelationParams{ParentID: "p", ChildID: "c"})
	require.ErrorIs(t, err, assert.AnError)

	assert.Empty(t, stub.capturedInvocation.InvocationID)
	assert.Empty(t, stub.capturedRelation.ParentInvocationID)
}

func TestInvocationRegistry_resolveAPIFollowsTheServicesDomain(t *testing.T) {
	for _, tc := range []struct {
		domain, want string
	}{
		{"", "https://multiplatform-analytics.services.bitrise.io"},
		{"bitrise.dev", "https://multiplatform-analytics.services.bitrise.dev"},
	} {
		t.Run("domain="+tc.domain, func(t *testing.T) {
			reg := newTestRegistry(map[string]string{"BITRISE_BUILD_CACHE_SERVICES_DOMAIN": tc.domain})

			api, err := reg.resolveAPI(auth.Credential{Token: "t"}, auth.Origin{})
			require.NoError(t, err)
			client, ok := api.(*ccacheanalytics.Client)
			require.True(t, ok)
			assert.Equal(t, tc.want, client.BaseURL())
		})
	}
}
