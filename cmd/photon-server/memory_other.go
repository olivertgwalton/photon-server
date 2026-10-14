//go:build !darwin || cgo

package main

import "github.com/prometheus/client_golang/prometheus"

func memoryCollectors() []prometheus.Collector { return nil }
