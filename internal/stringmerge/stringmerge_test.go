package stringmerge

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChangeContentInBlock(t *testing.T) {
	theBlockStartPattern := "# [start] generated-by-bitrise-build-cache"
	theBlockEndPattern := "# [end] generated-by-bitrise-build-cache"
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
