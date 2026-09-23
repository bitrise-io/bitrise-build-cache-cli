//go:build unit

package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSanitizeToken(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr error
	}{
		{name: "plain PAT unchanged", in: "abcdef1234", want: "abcdef1234"},
		{name: "trailing newline trimmed", in: "abcdef1234\n", want: "abcdef1234"},
		{name: "leading and trailing spaces trimmed", in: "  abcdef1234  ", want: "abcdef1234"},
		{name: "CRLF trimmed", in: "abcdef1234\r\n", want: "abcdef1234"},
		{name: "all whitespace becomes empty, no error", in: " \t\n", want: ""},
		{name: "empty input stays empty, no error", in: "", want: ""},
		{
			name: "JWT unchanged",
			in:   "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ1cyJ9.sig-part_with-dashes.and_underscores",
			want: "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ1cyJ9.sig-part_with-dashes.and_underscores",
		},
		{name: "embedded control byte rejected", in: "tok\x01en", wantErr: ErrTokenNonPrintable},
		{name: "embedded newline in middle rejected", in: "to\nken", wantErr: ErrTokenNonPrintable},
		{name: "high-bit byte rejected", in: "tok\xffen", wantErr: ErrTokenNonPrintable},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := SanitizeToken(tc.in)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
