package hls

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

// Convert writes a file's video and audio, as planned, into one MP4 at dst for downloading, its
// index at the front so a player starts it before it has all of it. It reports how far it has got,
// from 0 to 1 of duration, as ffmpeg goes; an error from progress stops it.
func (h Hardware) Convert(ctx context.Context, ffmpeg string, src *os.File, video domain.VideoPlan, audio *domain.AudioPlan, duration time.Duration, dst string, progress func(float64) error) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	a := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-protocol_whitelist", "fd", "-fd", "3"}
	if video.Encode != nil {
		a = append(a, h.inputArgs(video.Codec)...)
	}
	a = append(a, "-i", "fd:")
	a = append(a, streamArgs(h, video, audio)...)
	a = append(a, "-f", "mp4", "-movflags", "+faststart", "-progress", "pipe:1", "-nostats", "-y", dst)
	cmd := exec.CommandContext(ctx, ffmpeg, a...)
	cmd.ExtraFiles = []*os.File{src}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr := &tail{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	lines := bufio.NewScanner(out)
	for lines.Scan() {
		// ffmpeg says "N/A" before its first frame.
		us, ok := strings.CutPrefix(lines.Text(), "out_time_us=")
		at, perr := strconv.ParseInt(us, 10, 64)
		if !ok || perr != nil || duration <= 0 {
			continue
		}
		if err := progress(min(max(float64(at)/float64(duration.Microseconds()), 0), 1)); err != nil {
			cancel(err)
			break
		}
	}
	err = cmd.Wait()
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	if err != nil {
		return fmt.Errorf("ffmpeg: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
