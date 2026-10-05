package domain

import (
	"fmt"
	"slices"
	"time"
)

// CreditKind is what a person did on a title.
type CreditKind string

const (
	CreditActor CreditKind = "actor"
	// CreditGuestStar is an actor in one episode alone.
	CreditGuestStar CreditKind = "guest_star"
	CreditDirector  CreditKind = "director"
	CreditWriter    CreditKind = "writer"
	CreditProducer  CreditKind = "producer"
	CreditComposer  CreditKind = "composer"
	// CreditCreator made the show.
	CreditCreator CreditKind = "creator"
)

func CreditKinds() []CreditKind {
	return []CreditKind{CreditActor, CreditGuestStar, CreditDirector, CreditWriter, CreditProducer, CreditComposer, CreditCreator}
}

func ParseCreditKind(s string) (CreditKind, error) {
	if v := CreditKind(s); slices.Contains(CreditKinds(), v) {
		return v, nil
	}
	return "", fmt.Errorf("credit %q is not one of %v", s, CreditKinds())
}

// Credit is a person's part in a title, as a source gives it: their name, ids and picture, what
// they did and as whom.
type Credit struct {
	Name  string
	IDs   map[Provider]string
	Photo string
	Kind  CreditKind
	// Role is the character an actor plays or the job a crew member did.
	Role string
}

// Person is what a provider knows of someone beyond their credits.
type Person struct {
	Name       string
	Biography  string
	Born, Died time.Time
	Birthplace string
	Photo      string
}

// CreditSources are the sources that give credits.
func CreditSources() []FieldSource {
	return []FieldSource{SourceNFO, SourceTMDB, SourceTVDB}
}
