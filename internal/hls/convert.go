package hls

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/media"
)

// convertStall is how long a conversion may go without getting further. ffmpeg says how far it has
// got twice a second even while its input is stuck, so it is the time said that is watched: past
// this the file's mount is taken to have stopped answering, rather than the job holding its lease
// for ever.
const convertStall = 5 * time.Minute

// Convert writes a file's video and audio, as planned, into one MP4 at dst for downloading, its
// index at the front so a player starts it before it has all of it. It reports how far it has got,
// from 0 to 1 of duration, as ffmpeg goes; an error from progress stops it.
func (h Hardware) Convert(ctx context.Context, ffmpeg string, src *os.File, video domain.VideoPlan, audio *domain.AudioPlan, duration time.Duration, dst string, progress func(float64) error) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	a := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-protocol_whitelist", "fd", "-fd", "3"}
	if video.Encode != nil {
		a = append(a, h.inputArgs(video.Codec, *video.Encode)...)
	}
	a = append(a, "-i", "fd:")
	a = append(a, streamArgs(h, video, audio)...)
	a = append(a, "-f", "mp4", "-movflags", "+faststart", "-progress", "pipe:1", "-nostats", "-y", dst)
	cmd := media.NewCommand(ctx, []*os.File{src}, ffmpeg, a...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	stall := time.AfterFunc(convertStall, func() {
		cancel(fmt.Errorf("ffmpeg got no further for %s", convertStall))
	})
	defer stall.Stop()
	var furthest int64
	lines := bufio.NewScanner(out)
	for lines.Scan() {
		// ffmpeg says "N/A" before its first frame.
		us, ok := strings.CutPrefix(lines.Text(), "out_time_us=")
		at, perr := strconv.ParseInt(us, 10, 64)
		if !ok || perr != nil {
			continue
		}
		if at > furthest {
			furthest = at
			stall.Reset(convertStall)
		}
		if duration <= 0 {
			continue
		}
		if err := progress(min(max(float64(at)/float64(duration.Microseconds()), 0), 1)); err != nil {
			cancel(err)
			break
		}
	}
	return cmd.Err(cmd.Wait())
}
