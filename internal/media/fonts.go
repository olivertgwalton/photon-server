package media

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"slices"
	"strings"
)

// Font is a font a file carries for its styled subtitles: its stream's index, and the extension
// of its kind (".ttf").
type Font struct {
	Index int
	Ext   string
}

// fontExts are the kinds of font a file carries, by the extension of the name it gives one.
var fontExts = []string{".ttf", ".otf", ".ttc", ".woff", ".woff2"}

// Fonts lists the fonts an open file carries, from its header: attachments FFmpeg names a font
// codec, or whose names say they are fonts, as Matroska files often call a font a stream of bytes.
func (t Tools) Fonts(ctx context.Context, f *os.File) ([]Font, error) {
	out, err := output(ctx, Foreground, PartRun, []*os.File{f}, t.FFprobe.Path,
		"-hide_banner", "-v", "error", "-protocol_whitelist", "fd", "-fd", "3",
		"-select_streams", "t", "-show_entries", "stream=index,codec_name:stream_tags=filename", "-of", "json", "-i", "fd:")
	if err != nil {
		return nil, fmt.Errorf("ffprobe: %w", err)
	}
	return parseFonts(out)
}

func parseFonts(out []byte) ([]Font, error) {
	var p struct {
		Streams []struct {
			Index     int               `json:"index"`
			CodecName string            `json:"codec_name"`
			Tags      map[string]string `json:"tags"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &p); err != nil {
		return nil, fmt.Errorf("ffprobe output: %w", err)
	}
	var fonts []Font
	for _, s := range p.Streams {
		ext := strings.ToLower(path.Ext(s.Tags["filename"]))
		switch s.CodecName {
		case "ttf", "otf":
			ext = "." + s.CodecName
		}
		if slices.Contains(fontExts, ext) {
			fonts = append(fonts, Font{Index: s.Index, Ext: ext})
		}
	}
	return fonts, nil
}
