package githubsummary

import (
	"fmt"
	"strings"
)

// Render turns a parsed build into the markdown GitHub shows on the job page.
func Render(s Summary) string {
	var b strings.Builder

	b.WriteString("## 🤖 Bitrise Build Cache\n\n")

	if s.Empty() {
		b.WriteString("The build produced no Bitrise Build Cache output. " +
			"Either the cache was not activated, or the build failed before it ran.\n")

		return b.String()
	}

	if s.HasTaskStats {
		b.WriteString(fmt.Sprintf("### %s of Gradle work avoided\n\n", pct(s.TaskHitRate)))
		b.WriteString("| | tasks |\n| --- | ---: |\n")
		b.WriteString(fmt.Sprintf("| From cache | %s |\n", thousands(s.TasksFromCache)))
		if s.TasksUpToDate > 0 {
			b.WriteString(fmt.Sprintf("| Up to date | %s |\n", thousands(s.TasksUpToDate)))
		}
		if s.TasksExecuted > 0 {
			b.WriteString(fmt.Sprintf("| Executed | %s |\n", thousands(s.TasksExecuted)))
		}
		b.WriteString(fmt.Sprintf("| **Actionable total** | **%s** |\n\n", thousands(s.TasksTotal)))
	}

	if s.HasCacheStats {
		b.WriteString("### Cache transfer\n\n")
		b.WriteString("| | |\n| --- | ---: |\n")
		b.WriteString(fmt.Sprintf("| Downloaded | %s |\n", FormatSize(s.DownloadedBytes)))
		b.WriteString(fmt.Sprintf("| Uploaded | %s |\n", FormatSize(s.UploadedBytes)))
		b.WriteString(fmt.Sprintf("| Blob hits | %s of %s (%s) |\n",
			thousands(s.BlobHits), thousands(s.BlobLookups), pct(s.BlobHitRate)))
		b.WriteString("\n")
	}

	if s.InvocationURL != "" {
		b.WriteString(fmt.Sprintf("[View the full invocation](%s)\n\n", s.InvocationURL))
	}

	var details []string
	if s.CacheEndpoint != "" {
		details = append(details, fmt.Sprintf("Endpoint: `%s`", s.CacheEndpoint))
	}
	if s.PluginVersion != "" {
		details = append(details, fmt.Sprintf("Plugin: `%s`", s.PluginVersion))
	}
	if s.InvocationID != "" {
		details = append(details, fmt.Sprintf("Invocation: `%s`", s.InvocationID))
	}
	if len(details) > 0 {
		b.WriteString("<sub>" + strings.Join(details, " · ") + "</sub>\n")
	}

	return b.String()
}

func pct(v float64) string {
	return fmt.Sprintf("%.1f%%", v)
}

// GitHub renders the summary in a narrow column, so long counts get separators.
func thousands(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}

	out := make([]byte, 0, len(s)+len(s)/3)
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}

	return string(out)
}
