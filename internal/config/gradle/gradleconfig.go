package gradleconfig

import (
	"bytes"
	_ "embed"
	"fmt"
	"path/filepath"
	"text/template"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
)

//go:embed initd.gradle.kts.gotemplate
var gradleTemplateText string

//nolint:gochecknoglobals
var (
	errFmtParseTemplate             = "generate init.gradle: parse template: %w"
	errFmtExecuteTemplate           = "generate init.gradle: execute template: %w"
	errFmtGradleGeneration          = "couldn't generate gradle init content: %w"
	errFmtEnsureGradleInitDirExists = "ensure ~/.gradle/init.d exists: %w"
	errFmtWritingGradleInitFile     = "write bitrise-build-cache.init.gradle.kts to %s, error: %w"
)

// Generate init.gradle content.
// Recommended to save the content into $HOME/.gradle/init.d/ instead of
// overwriting the $HOME/.gradle/init.gradle file.
func (inventory TemplateInventory) GenerateInitGradle(templateProxy utils.TemplateProxy) (string, error) {
	tmpl, err := templateProxy.Parse("init.gradle", gradleTemplateText)
	if err != nil {
		return "", fmt.Errorf(errFmtParseTemplate, err)
	}

	resultBuffer := bytes.Buffer{}
	if err = templateProxy.Execute(tmpl, &resultBuffer, inventory); err != nil {
		return "", fmt.Errorf(errFmtExecuteTemplate, err)
	}

	return resultBuffer.String(), nil
}

func (inventory TemplateInventory) WriteToGradleInit(
	logger log.Logger,
	gradleHomePath string,
	osProxy utils.OsProxy,
	templateProxy utils.TemplateProxy,
) error {
	logger.Infof("(i) Ensure ~/.gradle and ~/.gradle/init.d directories exist")
	gradleInitDPath := filepath.Join(gradleHomePath, "init.d")
	err := osProxy.MkdirAll(gradleInitDPath, 0o755) //nolint:mnd
	if err != nil {
		return fmt.Errorf(errFmtEnsureGradleInitDirExists, err)
	}

	logger.Infof("(i) Generate ~/.gradle/init.d/bitrise-build-cache.init.gradle.kts")
	initGradleContent, err := inventory.GenerateInitGradle(templateProxy)
	if err != nil {
		return fmt.Errorf(errFmtGradleGeneration, err)
	}

	logger.Infof("(i) Write ~/.gradle/init.d/bitrise-build-cache.init.gradle.kts")
	{
		initGradlePath := filepath.Join(gradleInitDPath, "bitrise-build-cache.init.gradle.kts")
		err = osProxy.WriteFile(initGradlePath, []byte(initGradleContent), 0o755) //nolint:gosec,mnd
		if err != nil {
			return fmt.Errorf(errFmtWritingGradleInitFile, initGradlePath, err)
		}
	}

	return nil
}

func GradleTemplateProxy() utils.TemplateProxy {
	return utils.TemplateProxy{
		Parse: func(name string, templateText string) (*template.Template, error) {
			funcMap := template.FuncMap{
				"hasDependencies": TemplateInventory.HasDependencies,
			}

			return template.New(name).Funcs(funcMap).Parse(templateText)
		},
		Execute: func(template *template.Template, buffer *bytes.Buffer, inventory interface{}) error {
			return template.Execute(buffer, inventory)
		},
	}
}
