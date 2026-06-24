package common

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// selectFromList lets the user pick one of items, returning the chosen 0-based
// index. On an interactive terminal it shows the huh selection UI (the same
// library the `activate --interactive` wizard uses); otherwise — pipes, CI,
// tests — it falls back to a plain numbered prompt read from stdin.
func selectFromList(cmd *cobra.Command, prompt string, items []string) (int, error) {
	if in, ok := cmd.InOrStdin().(*os.File); ok && term.IsTerminal(int(in.Fd())) {
		return huhSelect(prompt, items)
	}

	return numberedPrompt(cmd, prompt, items)
}

func huhSelect(prompt string, items []string) (int, error) {
	options := make([]huh.Option[int], len(items))
	for i, item := range items {
		options[i] = huh.NewOption(item, i)
	}

	choice := 0
	if err := huh.NewSelect[int]().
		Title(prompt).
		Options(options...).
		Value(&choice).
		Run(); err != nil {
		return 0, fmt.Errorf("interactive selection: %w", err)
	}

	return choice, nil
}

// numberedPrompt prints a numbered list and reads a 1-based choice from stdin.
// Used non-interactively (pipes) or when there's no terminal.
func numberedPrompt(cmd *cobra.Command, prompt string, items []string) (int, error) {
	stderr := cmd.ErrOrStderr()
	fmt.Fprintf(stderr, "\n%s\n", prompt)
	for i, item := range items {
		fmt.Fprintf(stderr, "  %d) %s\n", i+1, item)
	}
	fmt.Fprint(stderr, "Enter number: ")

	scanner := bufio.NewScanner(cmd.InOrStdin())
	if !scanner.Scan() {
		return 0, errors.New("no selection read")
	}
	text := strings.TrimSpace(scanner.Text())
	choice, err := strconv.Atoi(text)
	if err != nil || choice < 1 || choice > len(items) {
		return 0, fmt.Errorf("invalid selection %q", text)
	}

	return choice - 1, nil
}
