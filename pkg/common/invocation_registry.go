package common

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/analytics/multiplatform"
	ccacheanalytics "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/ccache/analytics"
	configcommon "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/common"
	multiplatformconfig "github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/config/multiplatform"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/consts"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
)

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// InvocationRegistryParams configures the InvocationRegistry.
type InvocationRegistryParams struct {
	// Envs is the set of environment variables used for metadata.
	// If nil, the current process environment is used.
	Envs map[string]string
}

// RegisterInvocationParams configures the RegisterMultiplatformInvocation operation.
type RegisterInvocationParams struct {
	// InvocationID to register (required).
	InvocationID string

	// BuildTool label for the invocation. Defaults to "multiplatform" if empty.
	BuildTool string
}

// RegisterRelationParams configures the RegisterRelation operation.
type RegisterRelationParams struct {
	// ParentID is the parent invocation ID (required).
	ParentID string

	// ChildID is the child invocation ID (required).
	ChildID string

	// BuildTool label for the relation. Defaults to "ccache" if empty.
	BuildTool string
}

// invocationsAPI handles invocation and relation registration with the analytics backend.
type invocationsAPI interface {
	PutInvocation(inv multiplatform.Invocation) error
	PutInvocationRelation(rel multiplatform.InvocationRelation) error
}

// InvocationRegistry manages invocation registration with the analytics backend.
type InvocationRegistry struct {
	config multiplatformconfig.Config
	params InvocationRegistryParams
	logger log.Logger

	// api handles invocation and relation registration. If nil, a production client is created.
	// Set in tests to inject mocks.
	api invocationsAPI
}

// NewInvocationRegistry returns an InvocationRegistry ready to register invocations
// and relations. Auth credentials and the debug-logging flag are read from the
// multiplatform analytics config file on disk (single canonical source).
func NewInvocationRegistry(params InvocationRegistryParams) (*InvocationRegistry, error) {
	if params.Envs == nil {
		params.Envs = utils.AllEnvs()
	}

	config, err := multiplatformconfig.ReadConfig(utils.DefaultOsProxy{}, utils.DefaultDecoderFactory{})
	if err != nil {
		return nil, fmt.Errorf("read multiplatform config: %w", err)
	}

	return &InvocationRegistry{
		config: config,
		params: params,
		logger: log.NewLogger(log.WithDebugLog(config.DebugLogging)),
	}, nil
}

// RegisterMultiplatformInvocation registers a multiplatform invocation with the analytics backend.
func (inv *InvocationRegistry) RegisterMultiplatformInvocation(ctx context.Context, params RegisterInvocationParams) error {
	buildTool := params.BuildTool
	if buildTool == "" {
		buildTool = "multiplatform"
	}

	api, err := inv.resolveAPI(inv.logger)
	if err != nil {
		return fmt.Errorf("create analytics client: %w", err)
	}

	commandFunc := newCommandFunc(ctx)
	metadata := configcommon.NewMetadata(inv.params.Envs, commandFunc, inv.logger)

	invocation := multiplatform.NewInvocation(multiplatform.InvocationRunStats{
		InvocationID:   params.InvocationID,
		InvocationDate: time.Now(),
		BuildTool:      buildTool,
	}, inv.config.AuthConfig, metadata)

	if err := api.PutInvocation(*invocation); err != nil {
		return fmt.Errorf("register invocation: %w", err)
	}

	return nil
}

// RegisterRelation registers a parent→child relationship between two
// invocation IDs with the analytics backend.
func (inv *InvocationRegistry) RegisterRelation(ctx context.Context, params RegisterRelationParams) error {
	buildTool := params.BuildTool
	if buildTool == "" {
		buildTool = "ccache"
	}

	api, err := inv.resolveAPI(inv.logger)
	if err != nil {
		return fmt.Errorf("create analytics client: %w", err)
	}

	rel := multiplatform.InvocationRelation{
		ParentInvocationID: params.ParentID,
		ChildInvocationID:  params.ChildID,
		InvocationDate:     time.Now(),
		BuildTool:          buildTool,
	}

	if err := api.PutInvocationRelation(rel); err != nil {
		return fmt.Errorf("register invocation relation: %w", err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Private — InvocationRegistry methods
// ---------------------------------------------------------------------------

func (inv *InvocationRegistry) resolveAPI(logger log.Logger) (invocationsAPI, error) {
	if inv.api != nil {
		return inv.api, nil
	}

	client, err := ccacheanalytics.NewClient(consts.MultiplatformAnalyticsServiceEndpoint, inv.config.AuthConfig.TokenInGradleFormat(), logger)
	if err != nil {
		return nil, fmt.Errorf("new analytics client: %w", err)
	}

	return client, nil
}

func newCommandFunc(ctx context.Context) configcommon.CommandFunc {
	return func(name string, args ...string) (string, error) {
		output, err := exec.CommandContext(ctx, name, args...).Output() //nolint:gosec

		return string(output), err
	}
}
