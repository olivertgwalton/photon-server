package domain

import "testing"

func TestContainerName(t *testing.T) {
	for format, want := range map[string]string{
		"matroska,webm":           "mkv",
		"mov,mp4,m4a,3gp,3g2,mj2": "mp4",
		"mpegts":                  "ts",
		"avi":                     "avi",
		"asf":                     "asf",
		"":                        "",
	} {
		if got := ContainerName(format); got != want {
			t.Errorf("ContainerName(%q) = %q, want %q", format, got, want)
		}
	}
}
