package media

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeTool(t *testing.T, name, firstLine string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	script := "#!/bin/sh\necho '" + firstLine + "'\necho 'built with Apple clang'\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
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
		{name: "older major", ffmpeg: "ffmpeg version 8.1.3-Jellyfin Copyright", wantErr: "older than 9"},
		{name: "master snapshot", ffmpeg: "ffmpeg version N-121000-g1a2b3c4 Copyright", wantErr: "not a release build"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PHOTON_FFMPEG", fakeTool(t, "ffmpeg", tt.ffmpeg))
			t.Setenv("PHOTON_FFPROBE", fakeTool(t, "ffprobe", "ffprobe version 9.0.2 Copyright"))

			tools, err := FindTools(t.Context())
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
	path := filepath.Join(t.TempDir(), "ffmpeg")
	script := "#!/bin/sh\necho 'dyld: Library not loaded: libplacebo.dylib' >&2\nexit 1\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PHOTON_FFMPEG", path)

	_, err := FindTools(t.Context())
	if err == nil || !strings.Contains(err.Error(), "libplacebo.dylib") {
		t.Fatalf("err = %v, want it to carry the tool's stderr", err)
	}
}
