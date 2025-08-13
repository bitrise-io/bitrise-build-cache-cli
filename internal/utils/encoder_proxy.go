package utils

import (
	"encoding/json"
	"io"
)

type Encoder interface {
	SetIndent(prefix, indent string)
	SetEscapeHTML(escape bool)
	Encode(data any) error
}

type EncoderFactory interface {
	Encoder(v io.Writer) Encoder
}

type DefaultEncoderFactory struct{}

func (factory DefaultEncoderFactory) Encoder(v io.Writer) Encoder {
	return json.NewEncoder(v)
}
