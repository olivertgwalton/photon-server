package media

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/olivertgwalton/photon-server/internal/testtool"
)

func fakeTool(t *testing.T, name, firstLine string) string {
	t.Helper()
	return testtool.Script(t, t.TempDir(), name, "echo '"+firstLine+"'\necho 'built with Apple clang'\n")
}

func TestFindTools(t *testing.T) {
	tests := []struct {
		name        string
		ffmpeg      string
		wantVersion string
		wantErr     string
	}{
		{name: "release", ffmpeg: "ffmpeg version 9.0.2 Copyright (c) 2000-2026 the FFmpeg developers", wantVersion: "9.0.2"},
		{name: "git tag build", ffmpeg: "ffmpeg version n9.0.2-12-gabc1234 Copyright", wantVersion: "9.0.2"},
		{name: "two-part release", ffmpeg: "ffmpeg version 10.1 Copyright", wantVersion: "10.1"},
		{name: "jellyfin-ffmpeg", ffmpeg: "ffmpeg version 8.1.3-Jellyfin Copyright (c) 2000-2026 the FFmpeg developers", wantVersion: "8.1.3"},
		{name: "older major", ffmpeg: "ffmpeg version 7.1.1 Copyright", wantErr: "older than 8"},
		{name: "master snapshot", ffmpeg: "ffmpeg version N-121000-g1a2b3c4 Copyright", wantErr: "not a release build"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tools, err := FindTools(t.Context(), ToolNames{
				FFmpeg: fakeTool(t, "ffmpeg", tt.ffmpeg), FFprobe: fakeTool(t, "ffprobe", "ffprobe version 9.0.2 Copyright"), YTDLP: "yt-dlp",
			})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tools.FFmpeg.Version != tt.wantVersion {
				t.Errorf("ffmpeg version = %q, want %q", tools.FFmpeg.Version, tt.wantVersion)
			}
		})
	}
}

func TestFindToolsReportsStderr(t *testing.T) {
	path := testtool.Script(t, t.TempDir(), "ffmpeg", "echo 'dyld: Library not loaded: libplacebo.dylib' >&2\nexit 1\n")
	_, err := FindTools(t.Context(), ToolNames{FFmpeg: path, FFprobe: "ffprobe", YTDLP: "yt-dlp"})
	if err == nil || !strings.Contains(err.Error(), "libplacebo.dylib") {
		t.Fatalf("err = %v, want it to carry the tool's stderr", err)
	}
}

// A tool stuck on a mount that stopped answering fails its job rather than holding it.
func TestAToolThatHangsIsStopped(t *testing.T) {
	path := testtool.Script(t, t.TempDir(), "ffprobe", "exec sleep 60\n")
	start := time.Now()
	_, err := output(t.Context(), Background, 100*time.Millisecond, path)
	if err == nil || !strings.Contains(err.Error(), "still running after 100ms") {
		t.Fatalf("err = %v, want it to say the tool ran too long", err)
	}
	if took := time.Since(start); took > stopGrace {
		t.Errorf("took %s to stop", took)
	}
}

// yt-dlp is reported with its version where it is installed, and is no reason not to start where
// it is not.
func TestYTDLPIsOptional(t *testing.T) {
	ffmpeg, ffprobe := fakeTool(t, "ffmpeg", "ffmpeg version 9.0.2 Copyright"), fakeTool(t, "ffprobe", "ffprobe version 9.0.2 Copyright")
	ytdlp := testtool.Script(t, t.TempDir(), "yt-dlp", "echo 2026.08.19\n")
	for path, want := range map[string]Tool{ytdlp: {Path: ytdlp, Version: "2026.08.19"}, filepath.Join(t.TempDir(), "none"): {}} {
		tools, err := FindTools(t.Context(), ToolNames{FFmpeg: ffmpeg, FFprobe: ffprobe, YTDLP: path})
		if err != nil || tools.YTDLP != want {
			t.Errorf("yt-dlp at %s: %+v, %v; want %+v", path, tools.YTDLP, err, want)
		}
	}
}

// A tool named by a relative path is run by its absolute one, wherever the server is working.
func TestLookAnswersAnAbsolutePath(t *testing.T) {
	path := fakeTool(t, "pg_dump", "pg_dump (PostgreSQL) 18.0")
	t.Chdir(filepath.Dir(path))
	got, err := Look("./pg_dump")
	if err != nil || got != path {
		t.Errorf("Look(./pg_dump) = %q, %v; want %q", got, err, path)
	}
}

// A release's own ffmpeg, beside the server, is run rather than one the system has on PATH.
func TestLookPrefersAToolBesideTheServer(t *testing.T) {
	bundled := fakeTool(t, "ffmpeg", "ffmpeg version 8.1.3-Jellyfin Copyright")
	t.Setenv("PATH", filepath.Dir(fakeTool(t, "ffmpeg", "ffmpeg version 9.0.2 Copyright")))
	got, err := look(filepath.Dir(bundled), "ffmpeg")
	if err != nil || got != bundled {
		t.Errorf("look(ffmpeg) = %q, %v; want %q", got, err, bundled)
	}
}
