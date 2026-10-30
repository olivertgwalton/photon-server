package store

import "testing"

// A manager held to a Common Sense age gives no profile it keeps an older one, or none.
func TestAManagerGivesNoCommonSenseAgeOlderThanItsOwn(t *testing.T) {
	age := func(n int) *int { return &n }
	manager := ProfileAccess{MaxCommonSenseAge: age(10)}
	for _, tc := range []struct {
		name string
		a    ProfileAccess
		want bool
	}{
		{"younger", ProfileAccess{MaxCommonSenseAge: age(8)}, true},
		{"the same", ProfileAccess{MaxCommonSenseAge: age(10)}, true},
		{"older", ProfileAccess{MaxCommonSenseAge: age(12)}, false},
		{"none", ProfileAccess{}, false},
	} {
		if got := tc.a.within(manager); got != tc.want {
			t.Errorf("%s: within = %v, want %v", tc.name, got, tc.want)
		}
	}
	if !(ProfileAccess{MaxCommonSenseAge: age(18)}).within(ProfileAccess{}) {
		t.Error("a manager held to nothing may give any Common Sense age")
	}
}
