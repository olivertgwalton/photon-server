package media

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"syscall"
	"time"
)

// minimumMajor is the oldest FFmpeg the server is run against: jellyfin-ffmpeg's 8.1, which the
// image installs.
const minimumMajor = 8

type Tools struct {
	FFmpeg  Tool
	FFprobe Tool
	// Chromaprint is whether FFmpeg can fingerprint sound, which finding a season's intros needs.
	Chromaprint bool
	// YTDLP fetches theme tunes from ThemerrDB's YouTube links; its Path is empty where there is
	// none, and no theme is fetched.
	YTDLP Tool
}

type Tool struct {
	Path    string
	Version string
}

func FindTools(ctx context.Context) (Tools, error) {
	ffmpeg, err := findTool(ctx, cmp.Or(os.Getenv("PHOTON_FFMPEG"), "ffmpeg"))
	if err != nil {
		return Tools{}, err
	}
	ffprobe, err := findTool(ctx, cmp.Or(os.Getenv("PHOTON_FFPROBE"), "ffprobe"))
	if err != nil {
		return Tools{}, err
	}
	ytdlp, err := findYTDLP(ctx, cmp.Or(os.Getenv("PHOTON_YTDLP"), "yt-dlp"))
	if err != nil {
		return Tools{}, err
	}
	return Tools{FFmpeg: ffmpeg, FFprobe: ffprobe, Chromaprint: hasChromaprint(ctx, ffmpeg.Path), YTDLP: ytdlp}, nil
}

// findYTDLP answers yt-dlp where it is installed, and nothing where it is not; one that will not
// say its version is broken.
func findYTDLP(ctx context.Context, name string) (Tool, error) {
	path, err := exec.LookPath(name)
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
		return Tool{}, nil
	}
	if err != nil {
		return Tool{}, err
	}
	out, err := output(ctx, PartRun, nil, path, "--version")
	if err != nil {
		return Tool{}, fmt.Errorf("%s --version: %w", path, err)
	}
	return Tool{Path: path, Version: string(bytes.TrimSpace(out))}, nil
}

func findTool(ctx context.Context, name string) (Tool, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return Tool{}, err
	}
	out, err := output(ctx, PartRun, nil, path, "-hide_banner", "-version")
	if err != nil {
		return Tool{}, fmt.Errorf("%s -version: %w", path, err)
	}
	version, err := releaseVersion(out)
	if err != nil {
		return Tool{}, fmt.Errorf("%s: %w", path, err)
	}
	return Tool{Path: path, Version: version}, nil
}

// A release prints "ffmpeg version 9.0.2" (or "n9.0.2" from a git tag); a snapshot of master
// prints "N-121000-g…", which says nothing about the API it was built from.
var versionLine = regexp.MustCompile(`^\w+ version n?((\d+)\.\d+(?:\.\d+)?)\b`)

func releaseVersion(out []byte) (string, error) {
	line, _, _ := bytes.Cut(out, []byte("\n"))
	m := versionLine.FindSubmatch(line)
	if m == nil {
		return "", fmt.Errorf("not a release build: %q", line)
	}
	if major, _ := strconv.Atoi(string(m[2])); major < minimumMajor {
		return "", fmt.Errorf("version %s is older than %d", m[1], minimumMajor)
	}
	return string(m[1]), nil
}

const (
	stopGrace = 5 * time.Second
	// PartRun is how long a tool reading part of a file (a probe, a picture, a stretch of sound) may
	// run. A network mount that stops answering blocks a read forever, and the job waiting on it
	// keeps renewing its lease; past this the mount is taken to have stopped.
	PartRun = 5 * time.Minute
	// slowestRead is the rate, in bytes a second, a tool reading a whole file is held to: it may run
	// PartRun and the file's size at this rate.
	slowestRead = 8 << 20
)

// WholeRun is how long a tool reading all of f may run.
func WholeRun(f *os.File) time.Duration {
	info, err := f.Stat()
	if err != nil {
		return PartRun
	}
	return PartRun + time.Duration(info.Size()/slowestRead)*time.Second
}

// Within is ctx ending once the tool at path has run for limit, saying so.
func Within(ctx context.Context, path string, limit time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeoutCause(ctx, limit, fmt.Errorf("%s still running after %s", filepath.Base(path), limit))
}

// Command is a tool run as every one is run: files are its descriptors from 3 up; once ctx ends it
// is asked to stop with SIGTERM, so ffmpeg can finish what it is writing, and killed after
// stopGrace; and the end of what it writes to stderr is kept for Err.
type Command struct {
	*exec.Cmd
	ctx    context.Context
	stderr tail
}

func NewCommand(ctx context.Context, files []*os.File, path string, args ...string) *Command {
	cmd := exec.CommandContext(ctx, path, args...) //nolint:gosec // path is the operator's configured tool; its callers build every argument
	cmd.ExtraFiles = files
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = stopGrace
	c := &Command{Cmd: cmd, ctx: ctx}
	cmd.Stderr = &c.stderr
	return c
}

// Err is what running c came to, err being what running it answered: why ctx ended where it did,
// else err with the last the tool said.
func (c *Command) Err(err error) error {
	if err == nil {
		return nil
	}
	if cause := context.Cause(c.ctx); cause != nil {
		return cause
	}
	if said := bytes.TrimSpace(c.stderr.b); len(said) > 0 {
		return fmt.Errorf("%s: %w: %s", filepath.Base(c.Path), err, said)
	}
	return fmt.Errorf("%s: %w", filepath.Base(c.Path), err)
}

// tail keeps the last of what a tool writes, where the reason it failed is.
type tail struct{ b []byte }

func (t *tail) Write(p []byte) (int, error) {
	t.b = append(t.b, p...)
	if len(t.b) > 2048 {
		t.b = t.b[len(t.b)-2048:]
	}
	return len(p), nil
}

// output runs a tool to completion within limit and returns its stdout.
func output(ctx context.Context, limit time.Duration, files []*os.File, path string, args ...string) ([]byte, error) {
	ctx, cancel := Within(ctx, path, limit)
	defer cancel()
	c := NewCommand(ctx, files, path, args...)
	out, err := c.Output()
	return out, c.Err(err)
}
