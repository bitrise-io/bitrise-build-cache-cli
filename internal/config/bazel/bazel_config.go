package bazelconfig

import (
	"bytes"
	_ "embed"
	"fmt"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/stringmerge"
	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
)

//go:embed bazelrc.gotemplate
var bazelrcTemplateText string

const (
	errFmtInvalidTemplate    = "generate bazelrc: invalid template: %w"
	errFmtBazelrcGeneration  = "couldn't generate bazelrc content: %w"
	errFmtWritingBazelrcFile = "write bazelrc to %s, error: %w"
)

// GenerateBazelrc creates the bazelrc content from the inventory data
func (inventory TemplateInventory) GenerateBazelrc(templateProxy utils.TemplateProxy) (string, error) {
	tmpl, err := templateProxy.Parse("bazelrc", bazelrcTemplateText)
	if err != nil {
		return "", fmt.Errorf(errFmtInvalidTemplate, err)
	}

	resultBuffer := bytes.Buffer{}
	if err = templateProxy.Execute(tmpl, &resultBuffer, inventory); err != nil {
		return "", fmt.Errorf(errFmtBazelrcGeneration, err)
	}

	return resultBuffer.String(), nil
}

const (
	bazelBlockStart = "# [start] generated-by-bitrise-build-cache"
	bazelBlockEnd   = "# [end] generated-by-bitrise-build-cache"
)

// WriteToBazelrc writes the Bazel configuration to the specified bazelrc file. If the file exists, it only appends the
// generated content within the specified block. If it does not exist, it creates a new file with the content.
// Previously written content will be updated.
func (inventory TemplateInventory) WriteToBazelrc(
	logger log.Logger,
	bazelrcPath string,
	osProxy utils.OsProxy,
	templateProxy utils.TemplateProxy,
) error {
	logger.Infof("(i) Generate bazel configuration")
	bazelrcContent, err := inventory.GenerateBazelrc(templateProxy)
	if err != nil {
		return fmt.Errorf(errFmtBazelrcGeneration, err)
	}

	currentContent, exists, err := osProxy.ReadFileIfExists(bazelrcPath)
	if err != nil {
		return fmt.Errorf("reading .bazelrc: %w", err)
	}
	if !exists {
		currentContent = ""
	}

	finalContent := stringmerge.ChangeContentInBlock(currentContent, bazelBlockStart, bazelBlockEnd, bazelrcContent)

	logger.Infof("(i) Write bazel configuration to %s", bazelrcPath)
	if err = osProxy.WriteFile(bazelrcPath, []byte(finalContent), 0o644); err != nil {
		return fmt.Errorf(errFmtWritingBazelrcFile, bazelrcPath, err)
	}

	return nil
}
