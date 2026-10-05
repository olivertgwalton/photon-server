package hls

import (
	"bytes"
	"testing"

	"golang.org/x/text/encoding/charmap"
)

func TestASubtitlesCharsetIsItsMarkItsUTF8OrItsLanguages(t *testing.T) {
	cyrillic, err := charmap.Windows1251.NewEncoder().Bytes([]byte("Привет"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, lang string
		text       []byte
		want       string
	}{
		{"UTF-8", "ru", []byte("Привет"), ""},
		{"a UTF-8 mark", "", append([]byte{0xEF, 0xBB, 0xBF}, "Hi"...), ""},
		{"a UTF-16 mark", "en", []byte{0xFF, 0xFE, 'H', 0}, "UTF-16LE"},
		{"Russian in its codepage", "ru", cyrillic, "CP1251"},
		{"Traditional Chinese", "zh-Hant", []byte{0xA7, 0x41}, "BIG5"},
		{"Simplified Chinese", "zh", []byte{0xC4, 0xE3}, "GBK"},
		{"no language", "", []byte{0xE9}, "CP1252"},
	} {
		if got, err := subtitleCharset(bytes.NewReader(tc.text), tc.lang); err != nil || got != tc.want {
			t.Errorf("%s: %q, %v; want %q", tc.name, got, err, tc.want)
		}
	}
}
