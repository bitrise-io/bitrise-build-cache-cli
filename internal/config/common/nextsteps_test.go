//go:build unit

package common

import (
	"fmt"
	"strings"
	"testing"

	"github.com/bitrise-io/go-utils/v2/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// capturingLogger records every Println / Printf line so the test can assert
// on the banner shape.
type capturingLogger struct {
	log.Logger
	lines []string
}

func (c *capturingLogger) Printf(format string, args ...any) {
	c.lines = append(c.lines, fmt.Sprintf(format, args...))
}

func (c *capturingLogger) Println() {
	c.lines = append(c.lines, "")
}

func TestPrintNextSteps(t *testing.T) {
	type expect struct {
		mustContain    []string
		mustNotContain []string
	}

	tests := []struct {
		name    string
		bullets []string
		docsURL string
		expect  expect
	}{
		{
			name:    "empty docsURL omits See-for-details line",
			bullets: []string{"only bullet"},
			docsURL: "",
			expect: expect{
				mustContain:    []string{" 1. only bullet", "Next steps"},
				mustNotContain: []string{"See ", "for details."},
			},
		},
		{
			name:    "non-empty docsURL appends See-for-details line",
			bullets: []string{"only bullet"},
			docsURL: "https://docs.example.com/page.html",
			expect: expect{
				mustContain: []string{
					" 1. only bullet",
					" See https://docs.example.com/page.html for details.",
				},
			},
		},
		{
			name:    "multiple bullets numbered 1., 2., 3.",
			bullets: []string{"first", "second", "third"},
			docsURL: "",
			expect: expect{
				mustContain: []string{" 1. first", " 2. second", " 3. third"},
			},
		},
		{
			name:    "border frame present",
			bullets: []string{"bullet"},
			docsURL: "",
			expect: expect{
				mustContain: []string{strings.Repeat("─", 60)},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cl := &capturingLogger{Logger: log.NewLogger()}
			PrintNextSteps(cl, tc.bullets, tc.docsURL)

			out := strings.Join(cl.lines, "\n")
			require.NotEmpty(t, out)

			for _, s := range tc.expect.mustContain {
				assert.Contains(t, out, s)
			}
			for _, s := range tc.expect.mustNotContain {
				assert.NotContains(t, out, s)
			}
		})
	}
}
