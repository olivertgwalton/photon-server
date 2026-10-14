package hls

import (
	"bytes"
	"errors"
	"io"
	"unicode/utf8"

	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/language"
)

// maxSubtitleBytes is the most of a subtitle file read for its text: far more than any film's.
const maxSubtitleBytes = 16 << 20

var errSubtitleTooLarge = errors.New("subtitle file larger than 16 MiB")

// codepages are what subtitles in a language were written in before UTF-8, by names iconv and the
// WHATWG's encoding index share, as players fall back on the reader's locale: Windows' for Europe and the Middle East, the
// national ones for East Asia.
var codepages = map[string]string{
	"ru": "CP1251", "uk": "CP1251", "be": "CP1251", "bg": "CP1251", "mk": "CP1251", "sr": "CP1251",
	"pl": "CP1250", "cs": "CP1250", "sk": "CP1250", "hu": "CP1250", "ro": "CP1250", "hr": "CP1250", "sl": "CP1250", "bs": "CP1250",
	"el": "CP1253", "tr": "CP1254", "he": "CP1255", "ar": "CP1256", "fa": "CP1256",
	"lt": "CP1257", "lv": "CP1257", "et": "CP1257", "vi": "CP1258", "th": "WINDOWS-874",
	"ja": "SHIFT_JIS", "ko": "EUC-KR", "zh": "GBK",
}

// subtitleCharset says what a subtitle file's text is written in, for FFmpeg to read it as: what
// its byte order mark says, else UTF-8 if it is valid UTF-8, else the codepage of its language,
// else Windows' Western European. Empty is UTF-8, FFmpeg's own reading.
func subtitleCharset(r io.Reader, lang string) (string, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxSubtitleBytes))
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

// UTF8Subtitle reads a text subtitle file as UTF-8 without a byte order mark, as libass and a
// browser read it, from what subtitleCharset says it is written in, as Jellyfin re-encodes one it
// serves. Nil is a file in UTF-8 already, rewound to be served as it is.
func UTF8Subtitle(f io.ReadSeeker, lang string) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(f, maxSubtitleBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxSubtitleBytes {
		return nil, errSubtitleTooLarge
	}
	charset, err := subtitleCharset(bytes.NewReader(b), lang)
	if err != nil {
		return nil, err
	}
	if charset == "" {
		_, err := f.Seek(0, io.SeekStart)
		return nil, err
	}
	enc, err := htmlindex.Get(charset)
	if err != nil {
		return nil, err
	}
	text, err := enc.NewDecoder().Bytes(b)
	// UTF-16 is decoded with its mark, which a UTF-8 reader takes for text.
	return bytes.TrimPrefix(text, []byte("\uFEFF")), err
}
