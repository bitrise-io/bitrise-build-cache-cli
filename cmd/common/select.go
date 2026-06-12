package common

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	// errSelectCancelled is returned when the user aborts the picker (Esc / Ctrl-C / q).
	errSelectCancelled = errors.New("selection cancelled")
	// errRawUnsupported means the terminal can't enter raw mode; the caller
	// should fall back to the plain numbered prompt.
	errRawUnsupported = errors.New("interactive selection not supported on this terminal")
)

// selectFromList lets the user pick one of items. On a real terminal it shows
// an interactive list (↑/↓ to move, type a number to jump, Enter to select);
// otherwise — or if raw mode is unavailable — it falls back to a plain numbered
// prompt. Returns the chosen 0-based index.
func selectFromList(cmd *cobra.Command, prompt string, items []string) (int, error) {
	if in, ok := cmd.InOrStdin().(*os.File); ok {
		idx, err := runInteractiveSelect(in, cmd.ErrOrStderr(), prompt, items)
		if err == nil {
			return idx, nil
		}
		if !errors.Is(err, errRawUnsupported) {
			return 0, err // a real read error or a deliberate cancel — don't silently fall back
		}
	}

	return numberedPrompt(cmd, prompt, items)
}

// numberedPrompt prints a numbered list and reads a 1-based choice from stdin.
// Used non-interactively (pipes) or when raw mode isn't available.
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

// keyKind is a decoded keypress from the raw terminal input.
type keyKind int

const (
	keyOther keyKind = iota
	keyUp
	keyDown
	keyEnter
	keyCancel
	keyBackspace
	keyDigit
)

type keyEvent struct {
	kind  keyKind
	digit byte // valid when kind == keyDigit
}

// decodeKeys splits a burst of raw bytes (one or more keypresses) into events.
// Arrow keys arrive as a 3-byte escape sequence (ESC [ A/B); everything else is
// decoded byte by byte so fast/batched input isn't dropped.
func decodeKeys(b []byte) []keyEvent {
	var out []keyEvent
	for i := 0; i < len(b); i++ {
		if b[i] == 0x1b {
			if i+2 < len(b) && b[i+1] == '[' {
				switch b[i+2] {
				case 'A':
					out = append(out, keyEvent{kind: keyUp})
					i += 2

					continue
				case 'B':
					out = append(out, keyEvent{kind: keyDown})
					i += 2

					continue
				}
			}
			out = append(out, keyEvent{kind: keyCancel}) // lone Esc

			continue
		}
		out = append(out, decodeByte(b[i]))
	}

	return out
}

func decodeByte(c byte) keyEvent {
	switch {
	case c == '\r' || c == '\n':
		return keyEvent{kind: keyEnter}
	case c == 0x03 || c == 'q': // Ctrl-C, q
		return keyEvent{kind: keyCancel}
	case c == 0x7f || c == 0x08: // DEL, Backspace
		return keyEvent{kind: keyBackspace}
	case c == 'k':
		return keyEvent{kind: keyUp}
	case c == 'j':
		return keyEvent{kind: keyDown}
	case c >= '0' && c <= '9':
		return keyEvent{kind: keyDigit, digit: c}
	default:
		return keyEvent{kind: keyOther}
	}
}

// selector holds the interactive list state. It's decoupled from terminal I/O
// so the key-handling logic is unit-testable.
type selector struct {
	items  []string
	cursor int
	typed  string // accumulated digits for "type a number" selection
}

// handleKey applies one key event. The first result is true when the
// interaction finished; the second is then true if a choice was confirmed
// (cursor is the chosen index) or false if cancelled.
func (s *selector) handleKey(ev keyEvent) (bool, bool) {
	n := len(s.items)
	switch ev.kind {
	case keyUp:
		s.typed = ""
		s.cursor = (s.cursor - 1 + n) % n
	case keyDown:
		s.typed = ""
		s.cursor = (s.cursor + 1) % n
	case keyDigit:
		s.applyDigit(ev.digit)
	case keyBackspace:
		if len(s.typed) > 0 {
			s.typed = s.typed[:len(s.typed)-1]
			s.applyTyped()
		}
	case keyEnter:
		return true, true
	case keyCancel:
		return true, false
	case keyOther:
	}

	return false, false
}

// applyDigit appends a digit and moves the cursor to the typed 1-based index
// when it's in range; a value past the max restarts the buffer at that digit.
func (s *selector) applyDigit(d byte) {
	if v, err := strconv.Atoi(s.typed + string(d)); err == nil && v >= 1 && v <= len(s.items) {
		s.typed += string(d)
		s.cursor = v - 1

		return
	}
	if v, err := strconv.Atoi(string(d)); err == nil && v >= 1 && v <= len(s.items) {
		s.typed = string(d)
		s.cursor = v - 1

		return
	}
	s.typed = ""
}

func (s *selector) applyTyped() {
	if v, err := strconv.Atoi(s.typed); err == nil && v >= 1 && v <= len(s.items) {
		s.cursor = v - 1
	}
}

// runInteractiveSelect drives the raw-mode picker. Returns errRawUnsupported if
// the terminal can't enter raw mode (caller falls back), or errSelectCancelled
// if the user aborts.
func runInteractiveSelect(in *os.File, out io.Writer, prompt string, items []string) (int, error) {
	fd := int(in.Fd())
	if !term.IsTerminal(fd) {
		return 0, errRawUnsupported
	}
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return 0, errRawUnsupported
	}
	defer func() { _ = term.Restore(fd, oldState) }()

	s := &selector{items: items}
	renderSelector(out, prompt, s, false)

	buf := make([]byte, 16)
	for {
		n, rerr := in.Read(buf)
		if rerr != nil {
			fmt.Fprint(out, "\r\n")

			return 0, fmt.Errorf("read terminal input: %w", rerr)
		}
		for _, ev := range decodeKeys(buf[:n]) {
			done, ok := s.handleKey(ev)
			if done {
				renderSelector(out, prompt, s, true)
				fmt.Fprint(out, "\r\n")
				if !ok {
					return 0, errSelectCancelled
				}

				return s.cursor, nil
			}
		}
		renderSelector(out, prompt, s, true)
	}
}

// renderSelector draws (or redraws) the list. In raw mode newlines must be
// "\r\n"; on redraw it moves the cursor back up over the previously drawn lines
// and clears each one (\x1b[K) before reprinting.
func renderSelector(out io.Writer, prompt string, s *selector, redraw bool) {
	lines := len(s.items) + 2 // prompt + items + hint
	var b strings.Builder
	if redraw {
		fmt.Fprintf(&b, "\x1b[%dA", lines)
	}
	fmt.Fprintf(&b, "\r\x1b[K%s\r\n", prompt)
	for i, item := range s.items {
		marker := "  "
		if i == s.cursor {
			marker = "> "
		}
		fmt.Fprintf(&b, "\r\x1b[K%s%d) %s\r\n", marker, i+1, item)
	}
	hint := "Use ↑/↓ or type a number, Enter to select, Esc to cancel"
	if s.typed != "" {
		hint += "  [" + s.typed + "]"
	}
	fmt.Fprintf(&b, "\r\x1b[K%s\r\n", hint)

	fmt.Fprint(out, b.String())
}
