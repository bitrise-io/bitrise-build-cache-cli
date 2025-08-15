package utils

import (
	"encoding/json"
	"io"
)

//go:generate moq -out mocks/encoder_mock.go -pkg mocks . Encoder
type Encoder interface {
	SetIndent(prefix, indent string)
	SetEscapeHTML(escape bool)
	Encode(data any) error
}

//go:generate moq -out mocks/encoder_factory_mock.go -pkg mocks . EncoderFactory
type EncoderFactory interface {
	Encoder(w io.Writer) Encoder
}

type DefaultEncoderFactory struct{}

// Intentionally skipping interface return error - we are using this interface in many commands and their tests

//nolint:ireturn
func (factory DefaultEncoderFactory) Encoder(w io.Writer) Encoder {
	return json.NewEncoder(w)
}
