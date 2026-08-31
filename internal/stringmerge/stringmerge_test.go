package stringmerge

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

const (
	testBlockStart = "# [start] generated-by-bitrise-build-cache"
	testBlockEnd   = "# [end] generated-by-bitrise-build-cache"
)

func TestChangeContentInBlock(t *testing.T) {
	theBlockStartPattern := testBlockStart
	theBlockEndPattern := testBlockEnd
	theBlockContentStr := `org.gradle.caching=true
org.gradle.caching.debug=true`

	type args struct {
		currentContent    string
		blockStartPattern string
		blockEndPattern   string
		blockContentStr   string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "Empty current properties content",
			args: args{
				currentContent:    "",
				blockStartPattern: theBlockStartPattern,
				blockEndPattern:   theBlockEndPattern,
				blockContentStr:   theBlockContentStr,
			},
			want: `# [start] generated-by-bitrise-build-cache
org.gradle.caching=true
org.gradle.caching.debug=true
# [end] generated-by-bitrise-build-cache
`,
		},
		{
			name: "Non empty current properties content",
			args: args{
				currentContent: `org.gradle.caching.debug=true
org.gradle.configuration-cache=true`,
				blockStartPattern: theBlockStartPattern,
				blockEndPattern:   theBlockEndPattern,
				blockContentStr:   theBlockContentStr,
			},
			want: `org.gradle.caching.debug=true
org.gradle.configuration-cache=true
# [start] generated-by-bitrise-build-cache
org.gradle.caching=true
org.gradle.caching.debug=true
# [end] generated-by-bitrise-build-cache
`,
		},
		{
			name: "Existing build-cache block in current properties content",
			args: args{
				currentContent: `org.gradle.caching.debug=true
# [start] generated-by-bitrise-build-cache
REPLACETHIS
# [end] generated-by-bitrise-build-cache
org.gradle.configuration-cache=true`,
				blockStartPattern: theBlockStartPattern,
				blockEndPattern:   theBlockEndPattern,
				blockContentStr:   theBlockContentStr,
			},
			want: `org.gradle.caching.debug=true
# [start] generated-by-bitrise-build-cache
org.gradle.caching=true
org.gradle.caching.debug=true
# [end] generated-by-bitrise-build-cache
org.gradle.configuration-cache=true`,
		},
	}
	for _, tt := range tests { //nolint:varnamelen
		t.Run(tt.name, func(t *testing.T) {
			got := ChangeContentInBlock(tt.args.currentContent,
				tt.args.blockStartPattern,
				tt.args.blockEndPattern,
				tt.args.blockContentStr)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRemoveBlock(t *testing.T) {
	start := testBlockStart
	end := testBlockEnd

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "block absent",
			content: "user=alice\nother=stuff\n",
			want:    "user=alice\nother=stuff\n",
		},
		{
			name:    "empty content",
			content: "",
			want:    "",
		},
		{
			name: "block at start",
			content: `# [start] generated-by-bitrise-build-cache
managed=true
# [end] generated-by-bitrise-build-cache
user=alice
`,
			want: `user=alice
`,
		},
		{
			name: "block at end",
			content: `user=alice
# [start] generated-by-bitrise-build-cache
managed=true
# [end] generated-by-bitrise-build-cache
`,
			want: `user=alice
`,
		},
		{
			name: "block in the middle",
			content: `user=alice
# [start] generated-by-bitrise-build-cache
managed=true
# [end] generated-by-bitrise-build-cache
other=stuff
`,
			want: `user=alice
other=stuff
`,
		},
		{
			name:    "missing end marker leaves input unchanged",
			content: "user=alice\n# [start] generated-by-bitrise-build-cache\nmanaged=true\nother=stuff\n",
			want:    "user=alice\n# [start] generated-by-bitrise-build-cache\nmanaged=true\nother=stuff\n",
		},
		{
			name: "end marker before start marker leaves input unchanged",
			content: `# [end] generated-by-bitrise-build-cache
# [start] generated-by-bitrise-build-cache
`,
			want: `# [end] generated-by-bitrise-build-cache
# [start] generated-by-bitrise-build-cache
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RemoveBlock(tt.content, start, end)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestChangeAndRemoveBlock_RoundTrip(t *testing.T) {
	start := testBlockStart
	end := testBlockEnd

	cases := []string{
		"",
		"user=alice\n",
		"user=alice\nother=stuff\n",
	}
	for _, original := range cases {
		activated := ChangeContentInBlock(original, start, end, "managed=true")
		deactivated := RemoveBlock(activated, start, end)
		assert.Equalf(t, original, deactivated,
			"Activate→Deactivate must round-trip; original=%q, activated=%q", original, activated)
	}
}
