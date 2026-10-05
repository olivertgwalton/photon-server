package naming

import (
	"testing"
)

func TestStackPart(t *testing.T) {
	tests := []struct {
		stem string
		want Part
		ok   bool
	}{
		{"Neverland (2011)[720p][PG]part1", Part{Base: "Neverland (2011)[720p][PG]", Marker: "part", Number: 1}, true},
		{"Movie (2003) cd2", Part{Base: "Movie (2003)", Marker: "cd", Number: 2}, true},
		{"Movie - Disc 1", Part{Base: "Movie", Marker: "disc", Number: 1}, true},
		{"Movie [pt3]", Part{Base: "Movie", Marker: "pt", Number: 3}, true},
		{"Movie partc", Part{Base: "Movie", Marker: "part", Number: 3}, true},
		{"Movie parte", Part{}, false},
		{"Bad Boys (2006)", Part{}, false},
		{"Harry Potter and the Deathly Hallows 1", Part{}, false},
		{"Movie (2006).part1.stv.unrated.multi.1080p.bluray", Part{}, false},
		{"Moviecd1", Part{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.stem, func(t *testing.T) {
			got, ok := StackPart(tt.stem)
			if ok != tt.ok || got != tt.want {
				t.Errorf("StackPart(%q) = %+v, %v; want %+v, %v", tt.stem, got, ok, tt.want, tt.ok)
			}
		})
	}
}
