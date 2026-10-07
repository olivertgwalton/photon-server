package domain

import "time"

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

// Acting is whether the credit is a part played, not a job on the crew.
func (k CreditKind) Acting() bool {
	switch k {
	case CreditActor, CreditGuestStar:
		return true
	case CreditDirector, CreditWriter, CreditProducer, CreditComposer, CreditCreator:
	}
	return false
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
