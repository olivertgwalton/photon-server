package hls

import (
	"bytes"
	"io"
	"unicode/utf8"

	"golang.org/x/text/language"
)

// codepages are what subtitles in a language were written in before UTF-8, by iconv's
// names, as players fall back on the reader's locale: Windows' for Europe and the Middle East, the
// national ones for East Asia.
var codepages = map[string]string{
	"ru": "CP1251", "uk": "CP1251", "be": "CP1251", "bg": "CP1251", "mk": "CP1251", "sr": "CP1251",
	"pl": "CP1250", "cs": "CP1250", "sk": "CP1250", "hu": "CP1250", "ro": "CP1250", "hr": "CP1250", "sl": "CP1250", "bs": "CP1250",
	"el": "CP1253", "tr": "CP1254", "he": "CP1255", "ar": "CP1256", "fa": "CP1256",
	"lt": "CP1257", "lv": "CP1257", "et": "CP1257", "vi": "CP1258", "th": "CP874",
	"ja": "SHIFT_JIS", "ko": "EUC-KR", "zh": "GBK",
}

// subtitleCharset says what a subtitle file's text is written in, for FFmpeg to read it as: what
// its byte order mark says, else UTF-8 if it is valid UTF-8, else the codepage of its language,
// else Windows' Western European. Empty is UTF-8, FFmpeg's own reading.
func subtitleCharset(r io.Reader, lang string) (string, error) {
	b, err := io.ReadAll(io.LimitReader(r, 16<<20))
	if err != nil {
		return "", err
	}
	switch {
	case bytes.HasPrefix(b, []byte{0xEF, 0xBB, 0xBF}):
		return "", nil
	case bytes.HasPrefix(b, []byte{0xFF, 0xFE}):
		return "UTF-16LE", nil
	case bytes.HasPrefix(b, []byte{0xFE, 0xFF}):
		return "UTF-16BE", nil
	case utf8.Valid(b):
		return "", nil
	}
	// A language no one tagged, or none, is undetermined, and takes the default below.
	tag := language.Make(lang)
	base, _ := tag.Base()
	if base.String() == "zh" {
		if script, _ := tag.Script(); script.String() == "Hant" {
			return "BIG5", nil
		}
	}
	if cs, ok := codepages[base.String()]; ok {
		return cs, nil
	}
	return "CP1252", nil
}
