//go:build darwin && !cgo

package main

import (
	"os"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/prometheus/client_golang/prometheus"
)

// client_golang reads resident memory on macOS only through cgo, and the release builds have none,
// so it is read here from libSystem without it.
func memoryCollectors() []prometheus.Collector {
	lib, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		panic(err)
	}
	var procPidRusage func(pid int32, flavor int32, buffer unsafe.Pointer) int32
	purego.RegisterLibFunc(&procPidRusage, lib, "proc_pid_rusage")
	pid := int32(os.Getpid())
	return []prometheus.Collector{prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "process_resident_memory_bytes", Help: "Resident memory size in bytes.",
	}, func() float64 {
		var info rusageInfoV0
		procPidRusage(pid, 0, unsafe.Pointer(&info)) // 0 is RUSAGE_INFO_V0
		return float64(info.residentSize)
	})}
}

// rusageInfoV0 is <libproc.h>'s struct rusage_info_v0.
type rusageInfoV0 struct {
	uuid                           [16]byte
	userTime, systemTime           uint64
	pkgIdleWkups, interruptWkups   uint64
	pageins, wiredSize             uint64
	residentSize, physFootprint    uint64
	procStartAbstime, procExitTime uint64
}
