package interactive

import "context"

//go:generate moq -stub -out xcodebuild_info_mock_test.go -pkg interactive . XcodebuildInfoProvider

// XcodebuildInfoProvider sources picker candidates from xcodebuild. Scheme +
// configuration share a call because xcodebuild -list evaluates the whole
// project graph once per invocation.
type XcodebuildInfoProvider interface {
	ListSchemesAndConfigurations(ctx context.Context, workspace, project string) (schemes, configurations []string, err error)
	ShowDestinations(ctx context.Context, workspace, project, scheme string) ([]string, error)
}
