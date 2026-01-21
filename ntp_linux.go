//go:build linux

package main

import (
	"log"
	"math"
	"syscall"
	"unsafe"
)

// timex struct for adjtimex syscall
type timex struct {
	Modes     uint32
	Offset    int64
	Freq      int64
	Maxerror  int64
	Esterror  int64
	Status    int32
	Constant  int64
	Precision int64
	Tolerance int64
	Time      syscall.Timeval
	Tick      int64
	Ppsfreq   int64
	Jitter    int64
	Shift     int32
	Stabil    int64
	Jitcnt    int64
	Calcnt    int64
	Errcnt    int64
	Stbcnt    int64
	Tai       int32
	_         [44]byte // padding
}

// NTPStatus contains NTP synchronization information from adjtimex
type NTPStatus struct {
	Synced       bool    // Clock is synchronized
	ErrorMicros  int64   // Estimated error in microseconds
	ErrorSeconds float64 // Estimated error in seconds
}

// getNTPStatus returns detailed NTP synchronization status using adjtimex syscall
func getNTPStatus() NTPStatus {
	var tx timex

	r1, _, errno := syscall.Syscall(syscall.SYS_ADJTIMEX, uintptr(unsafe.Pointer(&tx)), 0, 0)
	if errno != 0 {
		log.Printf("adjtimex syscall failed: %v", errno)
		return NTPStatus{Synced: false, ErrorMicros: 1000000, ErrorSeconds: 1.0} // 1 second default error
	}

	status := int(r1)

	// TIME_ERROR (5) means clock is not synchronized
	const timeError = 5
	// STA_UNSYNC flag (bit 6) indicates unsynchronized state
	const staUnsync = 0x40

	// Clock is synchronized if:
	// 1. Return value is not TIME_ERROR (5)
	// 2. STA_UNSYNC flag is not set in tx.Status
	isSynced := status != timeError && (tx.Status&staUnsync) == 0

	// Esterror is in microseconds
	errorMicros := tx.Esterror
	if errorMicros <= 0 {
		errorMicros = 1000000 // Default to 1 second if not available
	}
	errorSeconds := float64(errorMicros) / 1e6

	log.Printf("NTP sync check: status=%d, tx.Status=0x%x, synced=%v, esterror=%d µs (%.6f s)",
		status, tx.Status, isSynced, errorMicros, errorSeconds)

	return NTPStatus{
		Synced:       isSynced,
		ErrorMicros:  errorMicros,
		ErrorSeconds: errorSeconds,
	}
}

// checkNTPSync checks if the local system clock is synchronized via NTP/PTP
func checkNTPSync() bool {
	return getNTPStatus().Synced
}

// calculateErrorEstimate creates the 16-bit TWAMP Error Estimate field
// Format (RFC 4656 Section 4.1.2):
//   Bit 15: S (Synchronized) - 1 if clock is synced to UTC via external source
//   Bit 14: Z (Zero) - 1 if timestamp is not available
//   Bits 8-13: Scale (6-bit unsigned)
//   Bits 0-7: Multiplier (8-bit unsigned)
// Error in seconds = Multiplier × 2^(-Scale)
func calculateErrorEstimate() uint16 {
	ntpStatus := getNTPStatus()

	// Calculate Scale and Multiplier from error
	// We want: errorSeconds ≈ Multiplier × 2^(-Scale)
	// Rearranging: Multiplier ≈ errorSeconds × 2^Scale
	//
	// Choose Scale to get a reasonable Multiplier (1-255)
	// Higher Scale = finer resolution

	errorSeconds := ntpStatus.ErrorSeconds

	// Limit error to reasonable range
	if errorSeconds < 0.000001 { // < 1 microsecond
		errorSeconds = 0.000001
	}
	if errorSeconds > 100 { // > 100 seconds
		errorSeconds = 100
	}

	// Find best Scale (0-63) that gives Multiplier in range 1-255
	var bestScale uint8 = 1
	var bestMultiplier uint8 = 1

	for scale := uint8(0); scale <= 63; scale++ {
		// Multiplier = errorSeconds × 2^Scale
		multiplier := errorSeconds * math.Pow(2, float64(scale))

		if multiplier >= 1 && multiplier <= 255 {
			bestScale = scale
			bestMultiplier = uint8(math.Round(multiplier))
			break
		}
	}

	// Build the Error Estimate field
	var errorEstimate uint16 = 0

	// Set S-bit if synchronized
	if ntpStatus.Synced {
		errorEstimate |= (1 << 15)
	}

	// Z-bit is 0 (timestamp is available)

	// Set Scale (bits 8-13)
	errorEstimate |= uint16(bestScale&0x3F) << 8

	// Set Multiplier (bits 0-7)
	errorEstimate |= uint16(bestMultiplier)

	// Calculate actual error for logging
	actualError := float64(bestMultiplier) * math.Pow(2, -float64(bestScale))

	log.Printf("TWAMP ErrorEstimate: synced=%v, targetError=%.6fs, scale=%d, mult=%d, actualError=%.6fs, value=0x%04X",
		ntpStatus.Synced, errorSeconds, bestScale, bestMultiplier, actualError, errorEstimate)

	return errorEstimate
}
