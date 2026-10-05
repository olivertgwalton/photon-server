package domain

import "testing"

func TestChapterMarker(t *testing.T) {
	for title, want := range map[string]MarkerKind{
		"Intro":              MarkerIntro,
		"Opening Credits":    MarkerIntro,
		"OP":                 MarkerIntro,
		"Chapter 2: Intro":   MarkerIntro,
		"End Credits":        MarkerCredits,
		"ED":                 MarkerCredits,
		"Outro":              MarkerCredits,
		"Previously on Lost": MarkerRecap,
		"Recap":              MarkerRecap,
		"Next Episode":       MarkerPreview,
		"Preview":            MarkerPreview,
		"Intro End":          "",
		"Opening: End":       "",
		"Chapter 3":          "",
		"Introspection":      "",
		"Credited Scene":     "",
		"":                   "",
	} {
		if got, _ := ChapterMarker(title); got != want {
			t.Errorf("ChapterMarker(%q) = %q, want %q", title, got, want)
		}
	}
}
