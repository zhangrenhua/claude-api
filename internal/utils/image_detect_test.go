package utils

import (
	"encoding/base64"
	"testing"
)

func TestDetectImageFormatFromBase64(t *testing.T) {
	cases := []struct {
		name  string
		bytes []byte
		want  string
	}{
		{"jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0, 0, 0, 0, 0, 0, 0}, "image/jpeg"},
		{"png", []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 0}, "image/png"},
		{"gif87a", []byte{0x47, 0x49, 0x46, 0x38, 0x37, 0x61, 0, 0, 0, 0, 0, 0}, "image/gif"},
		{"webp", []byte{0x52, 0x49, 0x46, 0x46, 0, 0, 0, 0, 0x57, 0x45, 0x42, 0x50}, "image/webp"},
		{"bmp", []byte{0x42, 0x4D, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, "image/bmp"},
		{"garbage", []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DetectImageFormatFromBase64(base64.StdEncoding.EncodeToString(c.bytes))
			if got != c.want {
				t.Errorf("got %q want %q", got, c.want)
			}
		})
	}

	t.Run("invalid_base64", func(t *testing.T) {
		if got := DetectImageFormatFromBase64("!!!not base64!!!"); got != "" {
			t.Errorf("got %q want empty for invalid base64", got)
		}
	})
}
