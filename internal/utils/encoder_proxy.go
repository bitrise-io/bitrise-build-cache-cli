package utils

import (
	"encoding/json"
	"io"
)

//go:generate moq -rm -out ./encoder_mock.go . Encoder:MockEncoder
type Encoder interface {
	SetIndent(prefix, indent string)
	SetEscapeHTML(escape bool)
	Encode(data any) error
}

//go:generate moq -rm -out ./encoder_factory_mock.go . EncoderFactory:MockEncoderFactory
type EncoderFactory interface {
	Encoder(v io.Writer) Encoder
}

type DefaultEncoderFactory struct{}

//nolint:ireturn
func (factory DefaultEncoderFactory) Encoder(v io.Writer) Encoder {
	return json.NewEncoder(v)
}
