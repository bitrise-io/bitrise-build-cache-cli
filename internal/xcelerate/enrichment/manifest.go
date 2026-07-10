package enrichment

import (
	"fmt"
	"os"
	"strings"
	"time"

	"howett.net/plist"
)

type ManifestEntry struct {
	UUID       string
	ClassName  string
	Title      string
	Signature  string
	SchemeName string
	FileName   string
	Status     string
	Start      time.Time
	Stop       time.Time
}

type Command string

const (
	CommandBuild   Command = "build"
	CommandTest    Command = "test"
	CommandArchive Command = "archive"
	CommandUnknown Command = ""
)

func (m ManifestEntry) Command() Command {
	source := m.Signature
	if source == "" {
		source = m.Title
	}

	switch strings.ToLower(strings.SplitN(source, " ", 2)[0]) {
	case "build":
		return CommandBuild
	case "test":
		return CommandTest
	case "archive":
		return CommandArchive
	default:
		return CommandUnknown
	}
}

func (m ManifestEntry) Success() bool {
	return m.Status == "S"
}

func LoadManifest(path string) ([]ManifestEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open manifest: %w", err)
	}
	defer f.Close()

	var raw manifestFile
	if err := plist.NewDecoder(f).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}

	out := make([]ManifestEntry, 0, len(raw.Logs))

	for uuid, entry := range raw.Logs {
		out = append(out, ManifestEntry{
			UUID:       uuid,
			ClassName:  entry.ClassName,
			Title:      entry.Title,
			Signature:  entry.Signature,
			SchemeName: entry.SchemeName,
			FileName:   entry.FileName,
			Status:     entry.HighLevelStatus,
			Start:      cfAbsoluteToTime(entry.TimeStarted),
			Stop:       cfAbsoluteToTime(entry.TimeStopped),
		})
	}

	return out, nil
}

// CFAbsoluteTime zero = 2001-01-01 00:00:00 UTC.
const cfAbsoluteEpoch int64 = 978307200

func cfAbsoluteToTime(cfAbs float64) time.Time {
	if cfAbs == 0 {
		return time.Time{}
	}

	sec := int64(cfAbs)
	nsec := int64((cfAbs - float64(sec)) * 1e9)

	return time.Unix(sec+cfAbsoluteEpoch, nsec).UTC()
}

type manifestFile struct {
	Logs map[string]manifestLog `plist:"logs"`
}

type manifestLog struct {
	ClassName       string  `plist:"className"`
	Title           string  `plist:"title"`
	Signature       string  `plist:"signature"`
	SchemeName      string  `plist:"schemeIdentifier-schemeName"`
	FileName        string  `plist:"fileName"`
	HighLevelStatus string  `plist:"highLevelStatus"`
	TimeStarted     float64 `plist:"timeStartedRecording"`
	TimeStopped     float64 `plist:"timeStoppedRecording"`
}
