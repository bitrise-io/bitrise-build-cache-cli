package common

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/bitrise-io/go-utils/v2/log"

	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/auth/keychain"
	"github.com/bitrise-io/bitrise-build-cache-cli/v2/internal/utils"
)

// prompter shares one bufio.Reader across prompts — separate readers would buffer ahead and drop piped input.
type prompter struct {
	reader       *bufio.Reader
	out          io.Writer
	readPassword func() (string, error)
}

// newDefaultPrompter builds the non-TTY prompter. selectWizard already gated on
// !term.IsTerminal before reaching this code path, so readPassword never tries
// to mask — there's no terminal to mask against.
func newDefaultPrompter() *prompter {
	stdinReader := bufio.NewReader(os.Stdin)

	return &prompter{
		reader: stdinReader,
		out:    os.Stdout,
		readPassword: func() (string, error) {
			line, err := stdinReader.ReadString('\n')
			if err != nil && !errors.Is(err, io.EOF) {
				return "", fmt.Errorf("read auth token: %w", err)
			}

			return strings.TrimRight(line, "\r\n"), err
		},
	}
}

type plainWizard struct {
	prompter *prompter
}

func (w *plainWizard) Run(ctx context.Context) error {
	p := w.prompter
	logger := log.NewLogger(log.WithDebugLog(IsDebugLogMode))
	logger.TInfof("Bitrise Build Cache - interactive local setup")

	fmt.Fprintln(p.out)
	fmt.Fprintln(p.out, "This wizard will configure Bitrise Build Cache for a build tool on this machine.")
	fmt.Fprintln(p.out, "You can find your workspace ID and a personal access token at: https://app.bitrise.io")
	fmt.Fprintln(p.out)

	tool, err := promptTool(p)
	if err != nil {
		return err
	}

	workspaceID, authToken, err := resolveCredentials(p, keychain.New())
	if err != nil {
		return err
	}

	pushEnabled, err := promptPushEnabled(p)
	if err != nil {
		return err
	}

	envs := utils.AllEnvs()
	envs[envWorkspaceID] = workspaceID
	envs[envAuthToken] = authToken

	switch tool {
	case toolGradle:
		return runInteractiveGradle(logger, envs, pushEnabled)
	case toolBazel:
		return runInteractiveBazel(logger, envs, pushEnabled)
	case toolXcode:
		return runInteractiveXcode(ctx, logger, envs, pushEnabled)
	case toolCcache:
		return runInteractiveCcache(ctx, logger, envs, pushEnabled)
	default:
		return fmt.Errorf("unsupported tool: %s", tool)
	}
}

func resolveCredentials(p *prompter, kc keychainStore) (string, string, error) {
	creds, err := kc.Load()
	if err == nil && creds.AuthToken != "" && creds.WorkspaceID != "" {
		fmt.Fprintln(p.out, "Reusing credentials already stored in the OS keychain.")

		return creds.WorkspaceID, creds.AuthToken, nil
	}

	envToken := os.Getenv(envAuthToken)
	envWS := os.Getenv(envWorkspaceID)

	if envToken != "" && envWS != "" {
		fmt.Fprintln(p.out)
		fmt.Fprintln(p.out, "Found BITRISE_BUILD_CACHE_AUTH_TOKEN + BITRISE_BUILD_CACHE_WORKSPACE_ID in env.")
		fmt.Fprintln(p.out, "Importing them into the OS keychain so you can remove them from your shell rc files.")

		if err := kc.Save(keychain.Credentials{AuthToken: envToken, WorkspaceID: envWS}); err != nil {
			fmt.Fprintf(p.out, "Could not save to keychain (%v). Continuing with env values for this run only.\n", err)

			return envWS, envToken, nil
		}

		fmt.Fprintln(p.out, "✅ Credentials saved to the OS keychain.")
		fmt.Fprintln(p.out, "You can now remove BITRISE_BUILD_CACHE_AUTH_TOKEN + BITRISE_BUILD_CACHE_WORKSPACE_ID from your shell rc files.")

		return envWS, envToken, nil
	}

	workspaceID, err := promptRequiredLine(p, "Workspace ID")
	if err != nil {
		return "", "", err
	}

	authToken, err := promptRequiredSecret(p, "Auth token (input hidden)")
	if err != nil {
		return "", "", err
	}

	if err := kc.Save(keychain.Credentials{AuthToken: authToken, WorkspaceID: workspaceID}); err != nil {
		fmt.Fprintf(p.out, "Could not save credentials to the OS keychain (%v). Continuing with values for this run only.\n", err)
	} else {
		fmt.Fprintln(p.out, "✅ Credentials saved to the OS keychain. Future CLI runs will pick them up automatically.")
	}

	return workspaceID, authToken, nil
}

func promptPushEnabled(p *prompter) (bool, error) {
	fmt.Fprintln(p.out)
	fmt.Fprintln(p.out, "Cache access mode:")
	fmt.Fprintln(p.out, "  1) Pull only (recommended for local dev)")
	fmt.Fprintln(p.out, "  2) Pull and push")

	for {
		fmt.Fprint(p.out, "Enter 1-2 [default: 1]: ")

		line, err := p.reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return false, fmt.Errorf("read cache access mode: %w", err)
		}

		switch strings.TrimSpace(line) {
		case "", "1":
			return false, nil
		case "2":
			return true, nil
		}

		fmt.Fprintln(p.out, "Invalid selection. Please enter 1 or 2.")

		if errors.Is(err, io.EOF) {
			return false, errors.New("no cache access mode selected (stdin closed)")
		}
	}
}

func promptTool(p *prompter) (interactiveTool, error) {
	tools := []interactiveTool{toolGradle, toolBazel, toolXcode, toolCcache}

	fmt.Fprintln(p.out, "Which build tool would you like to set up?")

	for i, t := range tools {
		fmt.Fprintf(p.out, "  %d) %s\n", i+1, t)
	}

	for {
		fmt.Fprintf(p.out, "Enter 1-%d: ", len(tools))

		line, err := p.reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("read tool selection: %w", err)
		}

		line = strings.TrimSpace(line)
		idx, convErr := strconv.Atoi(line)

		if convErr != nil || idx < 1 || idx > len(tools) {
			fmt.Fprintf(p.out, "Invalid selection %q. Please enter a number between 1 and %d.\n", line, len(tools))

			if errors.Is(err, io.EOF) {
				return "", errors.New("no tool selected (stdin closed)")
			}

			continue
		}

		return tools[idx-1], nil
	}
}

func promptRequiredLine(p *prompter, label string) (string, error) {
	for {
		fmt.Fprintf(p.out, "%s: ", label)

		line, err := p.reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", fmt.Errorf("read %s: %w", label, err)
		}

		value := strings.TrimSpace(line)
		if value != "" {
			return value, nil
		}

		fmt.Fprintf(p.out, "%s cannot be empty.\n", label)

		if errors.Is(err, io.EOF) {
			return "", fmt.Errorf("%s not provided (stdin closed)", label)
		}
	}
}

func promptRequiredSecret(p *prompter, label string) (string, error) {
	for {
		fmt.Fprintf(p.out, "%s: ", label)

		value, err := p.readPassword()
		fmt.Fprintln(p.out)

		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}

		value = strings.TrimSpace(value)
		if value != "" {
			return value, nil
		}

		fmt.Fprintf(p.out, "%s cannot be empty.\n", label)

		if errors.Is(err, io.EOF) {
			return "", fmt.Errorf("%s not provided (stdin closed)", label)
		}
	}
}
