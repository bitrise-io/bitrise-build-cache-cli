package gradleconfig

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"github.com/bitrise-io/go-utils/v2/log"
)

//go:embed initd.gradle.kts.gotemplate
var gradleTemplateText string

//nolint:gochecknoglobals
var (
	errFmtInvalidTemplate           = "generate init.gradle: invalid template: %w"
	errFmtGradleGeneration          = "couldn't generate gradle init content: %w"
	errFmtEnsureGradleInitDirExists = "ensure ~/.gradle/init.d exists: %w"
	errFmtWritingGradleInitFile     = "write bitrise-build-cache.init.gradle.kts to %s, error: %w"
)

// Generate init.gradle content.
// Recommended to save the content into $HOME/.gradle/init.d/ instead of
// overwriting the $HOME/.gradle/init.gradle file.
func (inventory TemplateInventory) GenerateInitGradle(templateProxy TemplateProxy) (string, error) {
	tmpl, err := templateProxy.parse("init.gradle", gradleTemplateText)
	if err != nil {
		return "", fmt.Errorf(errFmtInvalidTemplate, err)
	}

	resultBuffer := bytes.Buffer{}
	if err = templateProxy.execute(tmpl, &resultBuffer, inventory); err != nil {
		return "", err
	}

	return resultBuffer.String(), nil
}

func (inventory TemplateInventory) WriteToGradleInit(
	logger log.Logger,
	gradleHomePath string,
	osProxy OsProxy,
	templateProxy TemplateProxy,
) error {
	logger.Infof("(i) Ensure ~/.gradle and ~/.gradle/init.d directories exist")
	gradleInitDPath := filepath.Join(gradleHomePath, "init.d")
	err := osProxy.MkdirAll(gradleInitDPath, 0755) //nolint:gomnd,mnd
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
		err = osProxy.WriteFile(initGradlePath, []byte(initGradleContent), 0755) //nolint:gosec,gomnd,mnd
		if err != nil {
			return fmt.Errorf(errFmtWritingGradleInitFile, initGradlePath, err)
		}
	}

	return nil
}

type TemplateProxy struct {
	parse   func(name string, templateText string) (*template.Template, error)
	execute func(*template.Template, *bytes.Buffer, TemplateInventory) error
}

func DefaultTemplateProxy() TemplateProxy {
	return TemplateProxy{
		parse: func(name string, templateText string) (*template.Template, error) {
			return template.New(name).Parse(templateText)
		},
		execute: func(template *template.Template, buffer *bytes.Buffer, inventory TemplateInventory) error {
			return template.Execute(buffer, inventory)
		},
	}
}

type OsProxy struct {
	MkdirAll  func(string, os.FileMode) error
	WriteFile func(string, []byte, os.FileMode) error
}

func DefaultOsProxy() OsProxy {
	return OsProxy{
		MkdirAll:  os.MkdirAll,
		WriteFile: os.WriteFile,
	}
}
