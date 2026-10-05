package media

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/language"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type Facts struct {
	Container   string
	Duration    time.Duration
	BitrateKbps int
	Streams     []Stream
	Chapters    []Chapter
}

type Stream struct {
	Index    int
	Kind     domain.StreamKind
	Codec    string
	Profile  string
	Language language.Tag
	Title    string

	Default         bool
	Forced          bool
	HearingImpaired bool
	Commentary      bool

	Width, Height int
	FrameRate     float64
	Range         domain.Range
	DolbyVision   *DolbyVision

	Channels      int
	ChannelLayout string
	SampleRate    int
	BitrateKbps   int
}

type DolbyVision struct {
	Profile, Level   int
	Compatibility    int // the base layer's signal: 0 none, 1 HDR10, 2 SDR, 4 HLG
	BaseLayer        bool
	EnhancementLayer bool
	RPU              bool
}

type Chapter struct {
	Start, End time.Duration
	Title      string
}

// Probe describes an open file. ffprobe reads it through the descriptor, never a path, and reads
// the first frame of each stream: FFmpeg 9 leaves a stream's colour unknown until a frame is
// decoded, and HDR10+ metadata is only ever on frames.
func (t Tools) Probe(ctx context.Context, f *os.File) (Facts, error) {
	out, err := output(ctx, []*os.File{f}, t.FFprobe.Path,
		"-hide_banner", "-v", "error", "-protocol_whitelist", "fd", "-fd", "3",
		"-print_format", "json", "-show_format", "-show_streams", "-show_chapters",
		"-show_frames", "-read_intervals", "%+#1", "-i", "fd:")
	if err != nil {
		return Facts{}, fmt.Errorf("ffprobe: %w", err)
	}
	return parseProbe(out)
}

type probeOutput struct {
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
		BitRate    string `json:"bit_rate"`
	} `json:"format"`
	Streams  []probeStream `json:"streams"`
	Chapters []struct {
		StartTime string            `json:"start_time"`
		EndTime   string            `json:"end_time"`
		Tags      map[string]string `json:"tags"`
	} `json:"chapters"`
	Frames []struct {
		StreamIndex   int             `json:"stream_index"`
		ColorTransfer string          `json:"color_transfer"`
		SideData      []probeSideData `json:"side_data_list"`
	} `json:"frames"`
}

type probeStream struct {
	Index         int               `json:"index"`
	CodecType     string            `json:"codec_type"`
	CodecName     string            `json:"codec_name"`
	Profile       string            `json:"profile"`
	Width         int               `json:"width"`
	Height        int               `json:"height"`
	AvgFrameRate  string            `json:"avg_frame_rate"`
	ColorTransfer string            `json:"color_transfer"`
	Channels      int               `json:"channels"`
	ChannelLayout string            `json:"channel_layout"`
	SampleRate    string            `json:"sample_rate"`
	BitRate       string            `json:"bit_rate"`
	Disposition   map[string]int    `json:"disposition"`
	Tags          map[string]string `json:"tags"`
	SideData      []probeSideData   `json:"side_data_list"`
}

type probeSideData struct {
	Type          string `json:"side_data_type"`
	Profile       int    `json:"dv_profile"`
	Level         int    `json:"dv_level"`
	Compatibility int    `json:"dv_bl_signal_compatibility_id"`
	BLPresent     int    `json:"bl_present_flag"`
	ELPresent     int    `json:"el_present_flag"`
	RPUPresent    int    `json:"rpu_present_flag"`
}

func parseProbe(out []byte) (Facts, error) {
	var p probeOutput
	if err := json.Unmarshal(out, &p); err != nil {
		return Facts{}, fmt.Errorf("ffprobe output: %w", err)
	}
	facts := Facts{
		Container:   p.Format.FormatName,
		Duration:    seconds(p.Format.Duration),
		BitrateKbps: kbps(p.Format.BitRate),
	}
	for _, s := range p.Streams {
		kind := domain.StreamKind(s.CodecType)
		switch kind {
		case domain.StreamVideo, domain.StreamAudio, domain.StreamSubtitle:
		default:
			continue
		}
		if kind == domain.StreamVideo && s.Disposition["attached_pic"] == 1 {
			continue
		}
		lang, _ := language.Parse(s.Tags["language"])
		st := Stream{
			Index:           s.Index,
			Kind:            kind,
			Codec:           s.CodecName,
			Profile:         s.Profile,
			Language:        lang,
			Title:           s.Tags["title"],
			Default:         s.Disposition["default"] == 1,
			Forced:          s.Disposition["forced"] == 1,
			HearingImpaired: s.Disposition["hearing_impaired"] == 1,
			Commentary:      s.Disposition["comment"] == 1,
			Channels:        s.Channels,
			ChannelLayout:   s.ChannelLayout,
			SampleRate:      atoi(s.SampleRate),
			BitrateKbps:     kbps(s.BitRate),
		}
		if kind == domain.StreamVideo {
			st.Width, st.Height = s.Width, s.Height
			st.FrameRate = rate(s.AvgFrameRate)
			transfer, frameSide := s.ColorTransfer, []probeSideData(nil)
			for _, f := range p.Frames {
				if f.StreamIndex == s.Index {
					transfer, frameSide = f.ColorTransfer, f.SideData
					break
				}
			}
			st.DolbyVision = dolbyVision(s.SideData)
			st.Range = videoRange(transfer, st.DolbyVision, frameSide)
		}
		facts.Streams = append(facts.Streams, st)
	}
	for _, c := range p.Chapters {
		facts.Chapters = append(facts.Chapters, Chapter{
			Start: seconds(c.StartTime), End: seconds(c.EndTime), Title: c.Tags["title"],
		})
	}
	return facts, nil
}

func dolbyVision(side []probeSideData) *DolbyVision {
	for _, d := range side {
		if d.Type == "DOVI configuration record" {
			return &DolbyVision{
				Profile: d.Profile, Level: d.Level, Compatibility: d.Compatibility,
				BaseLayer: d.BLPresent == 1, EnhancementLayer: d.ELPresent == 1, RPU: d.RPUPresent == 1,
			}
		}
	}
	return nil
}

func videoRange(transfer string, dv *DolbyVision, frameSide []probeSideData) domain.Range {
	if dv != nil {
		return domain.RangeDV
	}
	switch transfer {
	case "smpte2084":
		for _, d := range frameSide {
			if strings.Contains(d.Type, "SMPTE2094-40") {
				return domain.RangeHDR10Plus
			}
		}
		return domain.RangeHDR10
	case "arib-std-b67":
		return domain.RangeHLG
	}
	return domain.RangeSDR
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func seconds(s string) time.Duration {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return time.Duration(math.Round(f * float64(time.Second)))
}

func kbps(bps string) int {
	return int(math.Round(float64(atoi(bps)) / 1000))
}

func rate(r string) float64 {
	num, den, ok := strings.Cut(r, "/")
	n, d := atoi(num), atoi(den)
	if !ok || d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}
