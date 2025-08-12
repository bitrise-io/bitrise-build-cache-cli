package internal

//go:generate moq -out mocks/original_arg_provider.go -pkg mocks . OriginalArgProvider
type OriginalArgProvider interface {
	GetOriginalArgs() []string
}

var _ OriginalArgProvider = DefaultOriginalArgProvider{}

type DefaultOriginalArgProvider struct{}

func (provider DefaultOriginalArgProvider) GetOriginalArgs() []string {
	return []string{}
}
