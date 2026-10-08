package main

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// A node's metrics say which version it runs, beside the Go runtime's.
func TestMetricsSayTheVersionAndTheRuntime(t *testing.T) {
	reg := metrics("v1.2.3")
	want := `# HELP photon_build_info The version of photon-server this node runs.
# TYPE photon_build_info gauge
photon_build_info{version="v1.2.3"} 1
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(want), "photon_build_info"); err != nil {
		t.Error(err)
	}
	if n, err := testutil.GatherAndCount(reg, "go_goroutines"); err != nil || n != 1 {
		t.Errorf("go_goroutines: %d, %v", n, err)
	}
}
