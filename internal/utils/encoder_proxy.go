package utils

import (
	"encoding/json"
	"io"
)

type EncoderInterface interface {
	Encode(any) error
}

type EncoderProxy struct {
	Encoder EncoderInterface
}

func (e EncoderProxy) Encode(v any) error {
	return e.Encoder.Encode(v)
}

type EncoderProxyCreator func(v io.Writer) EncoderProxy

var DefaultEncoderProxyCreator EncoderProxyCreator = func(v io.Writer) EncoderProxy {
	encoder := json.NewEncoder(v)
	encoder.SetIndent("", "    ")
	encoder.SetEscapeHTML(false)

	return EncoderProxy{
		Encoder: encoder,
	}
}

type MockEncoder struct {
	MockEncode func(v any) error
}

func (m MockEncoder) Encode(v any) error {
	return m.MockEncode(v)
}
