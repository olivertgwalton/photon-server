package media

import (
	"bytes"
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
	// Libass is whether FFmpeg draws styled subtitles, which burning one into video needs.
	Libass bool
	// YTDLP fetches theme tunes from ThemerrDB's YouTube links; its Path is empty where there is
	// none, and no theme is fetched.
	YTDLP Tool
}

type Tool struct {
	Path    string
	Version string
}

// ToolNames are what the tools are named by: each a path, or a command found on PATH.
type ToolNames struct {
	FFmpeg, FFprobe, YTDLP string
}

// FindTools finds the tools names names, each an executable file it answers by its absolute path,
// and checks each is one the server can run: FFmpeg's a release no older than minimumMajor.
func FindTools(ctx context.Context, names ToolNames) (Tools, error) {
	ffmpeg, err := findTool(ctx, names.FFmpeg)
	if err != nil {
		return Tools{}, err
	}
	ffprobe, err := findTool(ctx, names.FFprobe)
	if err != nil {
		return Tools{}, err
	}
	ytdlp, err := findYTDLP(ctx, names.YTDLP)
	if err != nil {
		return Tools{}, err
	}
	return Tools{FFmpeg: ffmpeg, FFprobe: ffprobe, Chromaprint: hasChromaprint(ctx, ffmpeg.Path), Libass: hasLibass(ctx, ffmpeg.Path), YTDLP: ytdlp}, nil
}

// hasLibass reports whether FFmpeg has the subtitles filter, which only a build with libass has.
func hasLibass(ctx context.Context, ffmpeg string) bool {
	out, err := output(ctx, Foreground, PartRun, nil, ffmpeg, "-hide_banner", "-h", "filter=subtitles")
	return err == nil && bytes.Contains(out, []byte("Filter subtitles"))
}

// findYTDLP answers yt-dlp where it is installed, and nothing where it is not; one that will not
// say its version is broken.
func findYTDLP(ctx context.Context, name string) (Tool, error) {
	path, err := Look(name)
	if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
		return Tool{}, nil
	}
	if err != nil {
		return Tool{}, err
	}
	out, err := output(ctx, Foreground, PartRun, nil, path, "--version")
	if err != nil {
		return Tool{}, fmt.Errorf("%s --version: %w", path, err)
	}
	return Tool{Path: path, Version: string(bytes.TrimSpace(out))}, nil
}

// Look answers the executable file name names, a path or a command found on PATH, by its absolute
// path, so what runs is what was found however the working directory changes.
func Look(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", err
	}
	return filepath.Abs(path)
}

func findTool(ctx context.Context, name string) (Tool, error) {
	path, err := Look(name)
	if err != nil {
		return Tool{}, err
	}
	out, err := output(ctx, Foreground, PartRun, nil, path, "-hide_banner", "-version")
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
	major, err := strconv.Atoi(string(m[2]))
	if err != nil {
		return "", fmt.Errorf("version %s: %w", m[1], err)
	}
	if major < minimumMajor {
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

// Priority is how a tool's process is scheduled beside the server's own.
type Priority string

const (
	// Foreground is a tool a playback waits on: its remux or transcode, a subtitle served to its
	// player, and the server's checks of its tools as it starts.
	Foreground Priority = "foreground"
	// Background is every other tool that reads media, as Jellyfin lowers every ffmpeg and ffprobe
	// it runs but a playback's transcode: a scan's probe, previews, fingerprints, keyframe walks,
	// downloads' conversions and theme tunes.
	Background Priority = "background"
)

// backgroundNice is the niceness a Background tool's process is given as it starts: 10, which is
// what .NET makes of BelowNormal, the priority Jellyfin gives each such process as it starts it. It
// is fixed, as Jellyfin's is everywhere but trickplay.
const backgroundNice = 10

// Command is a tool run as every one is run: at its priority; files are its descriptors from 3 up;
// once ctx ends it is asked to stop with SIGTERM, so ffmpeg can finish what it is writing, and
// killed after stopGrace; and the end of what it writes to stderr is kept for Err. It is started by
// its own Start, Run and Output, which give a Background tool its priority.
type Command struct {
	*exec.Cmd
	ctx      context.Context
	priority Priority
	stderr   tail
}

func NewCommand(ctx context.Context, priority Priority, files []*os.File, path string, args ...string) *Command {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.ExtraFiles = files
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = stopGrace
	c := &Command{Cmd: cmd, ctx: ctx, priority: priority}
	cmd.Stderr = &c.stderr
	return c
}

// Start starts the tool and, for a Background one, lowers it to backgroundNice at once, as Jellyfin
// sets a process's priority class straight after starting it. A tool already gone, or one on a
// server already running lower than backgroundNice, runs as it is (see lower); one that cannot be
// lowered for any other reason is stopped, and Start answers why, with what stopping it said.
func (c *Command) Start() error {
	if err := c.Cmd.Start(); err != nil {
		return err
	}
	switch c.priority {
	case Foreground:
	case Background:
		if err := lower(c.Process.Pid); err != nil {
			return errors.Join(err, c.Process.Kill(), c.Wait())
		}
	}
	return nil
}

// Run starts the tool, as Start does, and waits for it.
func (c *Command) Run() error {
	if err := c.Start(); err != nil {
		return err
	}
	return c.Wait()
}

// Output runs the tool, as Run does, and answers what it wrote to stdout.
func (c *Command) Output() ([]byte, error) {
	var out bytes.Buffer
	c.Stdout = &out
	err := c.Run()
	return out.Bytes(), err
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

// output runs a tool to completion within limit, at priority, and returns its stdout.
func output(ctx context.Context, priority Priority, limit time.Duration, files []*os.File, path string, args ...string) ([]byte, error) {
	ctx, cancel := Within(ctx, path, limit)
	defer cancel()
	c := NewCommand(ctx, priority, files, path, args...)
	out, err := c.Output()
	return out, c.Err(err)
}
