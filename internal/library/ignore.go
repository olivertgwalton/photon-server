package library

import (
	"regexp"
	"strings"
)

// ignoreFile is a .ignore file's patterns, as Jellyfin reads them: gitignore's, each matched
// against a path relative to the folder holding the file, the last that matches deciding.
type ignoreFile struct {
	dir   string
	rules []ignoreRule
}

type ignoreRule struct {
	re      *regexp.Regexp
	negate  bool
	dirOnly bool
}

// parseIgnore reads a .ignore file in dir. ok is false where it holds no pattern, which hides the
// whole folder.
func parseIgnore(dir, content string) (ignoreFile, bool) {
	f := ignoreFile{dir: dir}
	for line := range strings.Lines(content) {
		p := strings.TrimSpace(line)
		if p == "" || strings.HasPrefix(p, "#") {
			continue
		}
		var r ignoreRule
		p, r.negate = strings.CutPrefix(p, "!")
		p, r.dirOnly = strings.CutSuffix(p, "/")
		// A pattern with a slash before its end is anchored to the folder; one without matches at
		// any depth.
		anchor := "^(?:.*/)?"
		if strings.Contains(p, "/") {
			anchor, p = "^", strings.TrimPrefix(p, "/")
		}
		re, err := regexp.Compile(anchor + globRegexp(p) + "$")
		// An invalid pattern is skipped, as Jellyfin skips it.
		if p == "" || err != nil {
			continue
		}
		r.re = re
		f.rules = append(f.rules, r)
	}
	return f, len(f.rules) > 0
}

// ignores reports whether a path relative to the library root is hidden.
func (f ignoreFile) ignores(rel string, isDir bool) bool {
	if f.dir != "." {
		rel = strings.TrimPrefix(rel, f.dir+"/")
	}
	hidden := false
	for _, r := range f.rules {
		if (!r.dirOnly || isDir) && r.re.MatchString(rel) {
			hidden = !r.negate
		}
	}
	return hidden
}

// globRegexp turns a gitignore glob into a regular expression: * and ? stay within a name, ** crosses
// folders, and a bracket is a class.
func globRegexp(glob string) string {
	var b strings.Builder
	for i := 0; i < len(glob); i++ {
		c := glob[i]
		switch {
		case strings.HasPrefix(glob[i:], "**/"):
			b.WriteString("(?:.*/)?")
			i += 2
		case strings.HasPrefix(glob[i:], "**"):
			b.WriteString(".*")
			i++
		case c == '*':
			b.WriteString("[^/]*")
		case c == '?':
			b.WriteString("[^/]")
		case c == '[':
			end := strings.IndexByte(glob[i+1:], ']')
			if end < 0 {
				b.WriteString(`\[`)
				continue
			}
			class := glob[i+1 : i+1+end]
			if rest, ok := strings.CutPrefix(class, "!"); ok {
				class = "^" + rest
			}
			b.WriteString("[" + class + "]")
			i += end + 1
		case c == '\\' && i+1 < len(glob):
			i++
			b.WriteString(regexp.QuoteMeta(glob[i : i+1]))
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	return b.String()
}
