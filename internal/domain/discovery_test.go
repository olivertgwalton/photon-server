package domain

import "testing"

func TestDiscoveryIsBroadcastOrOff(t *testing.T) {
	for _, s := range []string{"broadcast", "off"} {
		if _, err := Parse("discovery", s, Discoveries()); err != nil {
			t.Errorf("Parse(%q): %v", s, err)
		}
	}
	for _, s := range []string{"", "on", "Off", "multicast"} {
		if _, err := Parse("discovery", s, Discoveries()); err == nil {
			t.Errorf("%q was accepted", s)
		}
	}
}
