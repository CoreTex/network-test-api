//go:build !linux

package main

import "log"

// checkNTPSync returns false on non-Linux platforms since adjtimex is not available.
// The actual sync check will only work when running in a Linux Docker container.
func checkNTPSync() bool {
	log.Printf("NTP sync check: adjtimex not available on this platform, assuming false")
	return false
}

// calculateErrorEstimate returns a default Error Estimate for non-Linux platforms.
// Format: S=0 (not synced), Z=0, Scale=1, Multiplier=1 = 0.5 second error
func calculateErrorEstimate() uint16 {
	log.Printf("TWAMP ErrorEstimate: using default 0x0101 (not Linux)")
	return 0x0101 // Default: 0.5 second error, not synchronized
}
