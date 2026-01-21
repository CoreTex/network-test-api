package unit

import (
	"math"
	"testing"
)

// ErrorEstimateInfo represents parsed Error Estimate field (RFC 4656/5357)
type ErrorEstimateInfo struct {
	Synced       bool
	Unavailable  bool
	Scale        uint8
	Multiplier   uint8
	ErrorSeconds float64
}

// parseErrorEstimate extracts S, Z, Scale, and Multiplier from the 16-bit Error Estimate field
func parseErrorEstimate(ee uint16) ErrorEstimateInfo {
	info := ErrorEstimateInfo{
		Synced:      (ee>>15)&1 == 1,
		Unavailable: (ee>>14)&1 == 1,
		Scale:       uint8((ee >> 8) & 0x3F),
		Multiplier:  uint8(ee & 0xFF),
	}

	if info.Unavailable || info.Multiplier == 0 {
		info.ErrorSeconds = -1
	} else {
		info.ErrorSeconds = float64(info.Multiplier) * math.Pow(2, -float64(info.Scale))
	}

	return info
}

func TestParseErrorEstimate_SyncedClock(t *testing.T) {
	// S-bit set (bit 15), Scale=10, Multiplier=1 -> 0x8A01
	ee := uint16(0x8A01)
	info := parseErrorEstimate(ee)

	if !info.Synced {
		t.Errorf("Expected Synced=true, got false")
	}
	if info.Unavailable {
		t.Errorf("Expected Unavailable=false, got true")
	}
	if info.Scale != 10 {
		t.Errorf("Expected Scale=10, got %d", info.Scale)
	}
	if info.Multiplier != 1 {
		t.Errorf("Expected Multiplier=1, got %d", info.Multiplier)
	}

	// Error = 1 * 2^(-10) = 0.0009765625
	expectedError := 0.0009765625
	if math.Abs(info.ErrorSeconds-expectedError) > 1e-10 {
		t.Errorf("Expected ErrorSeconds=%v, got %v", expectedError, info.ErrorSeconds)
	}
}

func TestParseErrorEstimate_UnsyncedClock(t *testing.T) {
	// S-bit not set, Scale=5, Multiplier=135 -> 0x0587
	ee := uint16(0x0587)
	info := parseErrorEstimate(ee)

	if info.Synced {
		t.Errorf("Expected Synced=false, got true")
	}
	if info.Unavailable {
		t.Errorf("Expected Unavailable=false, got true")
	}
	if info.Scale != 5 {
		t.Errorf("Expected Scale=5, got %d", info.Scale)
	}
	if info.Multiplier != 135 {
		t.Errorf("Expected Multiplier=135, got %d", info.Multiplier)
	}

	// Error = 135 * 2^(-5) = 4.21875
	expectedError := 4.21875
	if math.Abs(info.ErrorSeconds-expectedError) > 1e-10 {
		t.Errorf("Expected ErrorSeconds=%v, got %v", expectedError, info.ErrorSeconds)
	}
}

func TestParseErrorEstimate_Unavailable(t *testing.T) {
	// Z-bit set (bit 14) -> timestamp unavailable
	ee := uint16(0x4001)
	info := parseErrorEstimate(ee)

	if info.Synced {
		t.Errorf("Expected Synced=false, got true")
	}
	if !info.Unavailable {
		t.Errorf("Expected Unavailable=true, got false")
	}
	if info.ErrorSeconds != -1 {
		t.Errorf("Expected ErrorSeconds=-1 (unavailable), got %v", info.ErrorSeconds)
	}
}

func TestParseErrorEstimate_ZeroMultiplier(t *testing.T) {
	// Multiplier=0 means unavailable regardless of Z-bit
	ee := uint16(0x8A00)
	info := parseErrorEstimate(ee)

	if !info.Synced {
		t.Errorf("Expected Synced=true, got false")
	}
	if info.Multiplier != 0 {
		t.Errorf("Expected Multiplier=0, got %d", info.Multiplier)
	}
	if info.ErrorSeconds != -1 {
		t.Errorf("Expected ErrorSeconds=-1 (zero multiplier), got %v", info.ErrorSeconds)
	}
}

func TestParseErrorEstimate_MaxScale(t *testing.T) {
	// Max scale = 63 (6 bits), Multiplier=1 -> very small error
	ee := uint16(0x3F01)
	info := parseErrorEstimate(ee)

	if info.Scale != 63 {
		t.Errorf("Expected Scale=63, got %d", info.Scale)
	}

	// Error = 1 * 2^(-63) = very small number
	expectedError := math.Pow(2, -63)
	if math.Abs(info.ErrorSeconds-expectedError) > 1e-25 {
		t.Errorf("Expected ErrorSeconds=%v, got %v", expectedError, info.ErrorSeconds)
	}
}

func TestParseErrorEstimate_ReflectorExample(t *testing.T) {
	// Real-world example from perfSONAR: 0x8587
	ee := uint16(0x8587)
	info := parseErrorEstimate(ee)

	if !info.Synced {
		t.Errorf("Expected Synced=true, got false")
	}
	if info.Scale != 5 {
		t.Errorf("Expected Scale=5, got %d", info.Scale)
	}
	if info.Multiplier != 135 {
		t.Errorf("Expected Multiplier=135, got %d", info.Multiplier)
	}
}

func TestParseErrorEstimate_BothFlags(t *testing.T) {
	// Both S and Z bits set -> 0xC001
	ee := uint16(0xC001)
	info := parseErrorEstimate(ee)

	if !info.Synced {
		t.Errorf("Expected Synced=true, got false")
	}
	if !info.Unavailable {
		t.Errorf("Expected Unavailable=true, got false")
	}
	if info.ErrorSeconds != -1 {
		t.Errorf("Expected ErrorSeconds=-1 (unavailable), got %v", info.ErrorSeconds)
	}
}
