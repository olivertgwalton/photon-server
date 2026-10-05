package domain

import "testing"

func TestParseDiscovery(t *testing.T) {
	for _, s := range []string{"broadcast", "off"} {
		if _, err := ParseDiscovery(s); err != nil {
			t.Errorf("ParseDiscovery(%q): %v", s, err)
		}
	}
	for _, s := range []string{"", "on", "Off", "multicast"} {
		if _, err := ParseDiscovery(s); err == nil {
			t.Errorf("ParseDiscovery(%q) was accepted", s)
		}
	}
}
