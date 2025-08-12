package utils

import (
	"encoding/json"
	"io"
)

type EncoderInterface interface {
	Encode(any) error
}

type EncoderFactory interface {
	Encoder(v io.Writer) EncoderInterface
}

type DefaultEncoderFactory struct{}

func (factory DefaultEncoderFactory) Encoder(v io.Writer) EncoderInterface {
	encoder := json.NewEncoder(v)
	encoder.SetIndent("", "    ")
	encoder.SetEscapeHTML(false)

	return encoder
}
