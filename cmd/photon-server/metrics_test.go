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

// Every release build reports the memory the server holds, macOS's without cgo among them.
func TestMetricsSayTheResidentMemory(t *testing.T) {
	families, err := metrics("v1.2.3").Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() == "process_resident_memory_bytes" {
			if v := f.GetMetric()[0].GetGauge().GetValue(); v <= 0 {
				t.Errorf("resident memory %v", v)
			}
			return
		}
	}
	t.Error("no process_resident_memory_bytes")
}
