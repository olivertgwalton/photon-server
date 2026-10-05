package media

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"syscall"
	"time"
)

const minimumMajor = 9

type Tools struct {
	FFmpeg  Tool
	FFprobe Tool
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
	return Tools{FFmpeg: ffmpeg, FFprobe: ffprobe}, nil
}

func findTool(ctx context.Context, name string) (Tool, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return Tool{}, err
	}
	out, err := output(ctx, nil, path, "-hide_banner", "-version")
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

const stopGrace = 5 * time.Second

// output runs a tool to completion and returns its stdout. files are the tool's descriptors from 3
// up. A cancelled context asks the process to stop with SIGTERM, so ffmpeg can finish what it is
// writing, and kills it after stopGrace.
func output(ctx context.Context, files []*os.File, path string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, path, args...) //nolint:gosec // path is the operator's configured tool; this package builds every argument
	cmd.ExtraFiles = files
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = stopGrace
	out, err := cmd.Output()
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok && len(exitErr.Stderr) > 0 {
		return out, fmt.Errorf("%w: %s", err, bytes.TrimSpace(exitErr.Stderr))
	}
	return out, err
}
