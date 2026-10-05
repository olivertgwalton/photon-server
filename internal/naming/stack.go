package naming

import (
	"regexp"
	"strings"
)

// Part is one file of a title split across several: "Movie (2011) part1", "Movie cd2".
type Part struct {
	Base   string // what every part of the stack shares
	Marker string // cd, dvd, part, pt, disc or disk; parts stack only with the same marker
	Number int
}

var partSuffix = regexp.MustCompile(`(?i)^(.*?)([ _.-]*)[\[(]?(cd|dvd|part|pt|disc|disk)[ _.-]*([0-9]+|[a-d])[\])]?$`)

// StackPart reads a part marker, which must end the stem and follow a separator or a closing
// bracket, so "Bad Boys (2006)" and "Bad Boys (2007)" are not two parts of one film.
func StackPart(stem string) (Part, bool) {
	m := partSuffix.FindStringSubmatch(stem)
	if m == nil || m[1] == "" {
		return Part{}, false
	}
	if m[2] == "" && !strings.ContainsAny(m[1][len(m[1])-1:], ")]}") {
		return Part{}, false
	}
	n := 0
	if c := strings.ToLower(m[4]); c[0] >= 'a' && c[0] <= 'd' {
		n = int(c[0]-'a') + 1
	} else {
		n = atoi(c)
	}
	return Part{Base: m[1], Marker: strings.ToLower(m[3]), Number: n}, true
}
