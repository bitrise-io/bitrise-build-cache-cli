package mocks

import (
	"io"

	"github.com/bitrise-io/bitrise-build-cache-cli/internal/utils"
)

type MockEncoder struct {
	MockEncode func(any) error
}

func (mock MockEncoder) Encode(data any) error {
	return mock.MockEncode(data)
}

type MockEncoderFactory struct {
	Mock MockEncoder
}

func (factory MockEncoderFactory) Encoder(v io.Writer) utils.EncoderInterface {
	return factory.Mock
}
