package domain

// Counting is whether a page of a list asks how many the list holds in all, which costs reading
// all of it: a wall asks once, for its first page, to size itself, and a row or a list read on
// asks never, being told by each page whether more follow it.
type Counting string

const (
	CountNone  Counting = "none"
	CountExact Counting = "exact"
)

func Countings() []Counting { return []Counting{CountNone, CountExact} }
