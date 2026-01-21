package unit

import (
	"math"
	"testing"
	"time"
)

// RFC 3393 IPDV (IP Packet Delay Variation) calculation
// IPDV(i) = D(i) - D(i-1) where D is one-way delay
func calculateIPDV(delays []time.Duration) []time.Duration {
	if len(delays) < 2 {
		return nil
	}
	ipdv := make([]time.Duration, len(delays)-1)
	for i := 1; i < len(delays); i++ {
		ipdv[i-1] = delays[i] - delays[i-1]
	}
	return ipdv
}

// RFC 3550 Jitter: exponentially smoothed mean absolute IPDV
// J(i) = J(i-1) + (|D(i,i-1)| - J(i-1)) / 16
func calculateRFC3550Jitter(ipdv []time.Duration) float64 {
	if len(ipdv) == 0 {
		return 0
	}
	jitter := float64(0)
	for _, d := range ipdv {
		absD := math.Abs(float64(d.Nanoseconds()))
		jitter = jitter + (absD-jitter)/16.0
	}
	return jitter
}

// Calculate hop count from TTL (forward direction)
// Sender uses TTL=255, forward hops = 255 - SenderTTL
func calculateForwardHops(senderTTL int) int {
	if senderTTL <= 0 || senderTTL > 255 {
		return -1
	}
	return 255 - senderTTL
}

// Calculate hop count from TTL (reverse direction)
// Estimate initial TTL based on received TTL
func calculateReverseHops(receivedTTL int) int {
	if receivedTTL <= 0 {
		return -1
	}
	var initialTTL int
	if receivedTTL > 128 {
		initialTTL = 255
	} else if receivedTTL > 64 {
		initialTTL = 128
	} else {
		initialTTL = 64
	}
	return initialTTL - receivedTTL
}

func TestCalculateIPDV_BasicSequence(t *testing.T) {
	delays := []time.Duration{
		10 * time.Millisecond,
		12 * time.Millisecond,
		11 * time.Millisecond,
		15 * time.Millisecond,
	}

	ipdv := calculateIPDV(delays)

	if len(ipdv) != 3 {
		t.Fatalf("Expected 3 IPDV values, got %d", len(ipdv))
	}

	expected := []time.Duration{
		2 * time.Millisecond,  // 12 - 10
		-1 * time.Millisecond, // 11 - 12
		4 * time.Millisecond,  // 15 - 11
	}

	for i, exp := range expected {
		if ipdv[i] != exp {
			t.Errorf("IPDV[%d]: expected %v, got %v", i, exp, ipdv[i])
		}
	}
}

func TestCalculateIPDV_EmptyInput(t *testing.T) {
	ipdv := calculateIPDV([]time.Duration{})
	if ipdv != nil {
		t.Errorf("Expected nil for empty input, got %v", ipdv)
	}
}

func TestCalculateIPDV_SingleDelay(t *testing.T) {
	ipdv := calculateIPDV([]time.Duration{10 * time.Millisecond})
	if ipdv != nil {
		t.Errorf("Expected nil for single delay, got %v", ipdv)
	}
}

func TestCalculateIPDV_ClockOffsetCancellation(t *testing.T) {
	// Simulate clock offset of 100ms
	// Raw delays include offset, but IPDV should be the same
	clockOffset := 100 * time.Millisecond

	actualDelays := []time.Duration{
		10 * time.Millisecond,
		12 * time.Millisecond,
		11 * time.Millisecond,
	}

	rawDelays := make([]time.Duration, len(actualDelays))
	for i, d := range actualDelays {
		rawDelays[i] = d + clockOffset
	}

	ipdvActual := calculateIPDV(actualDelays)
	ipdvRaw := calculateIPDV(rawDelays)

	// IPDV should be identical - clock offset cancels out!
	for i := range ipdvActual {
		if ipdvActual[i] != ipdvRaw[i] {
			t.Errorf("Clock offset did not cancel out at index %d: actual=%v, raw=%v",
				i, ipdvActual[i], ipdvRaw[i])
		}
	}
}

func TestCalculateRFC3550Jitter_BasicSequence(t *testing.T) {
	ipdv := []time.Duration{
		2 * time.Millisecond,
		-1 * time.Millisecond,
		4 * time.Millisecond,
		-2 * time.Millisecond,
	}

	jitter := calculateRFC3550Jitter(ipdv)

	// Verify jitter is positive (exponentially smoothed absolute IPDV)
	if jitter <= 0 {
		t.Errorf("Expected positive jitter, got %v", jitter)
	}

	// Jitter should be less than max absolute IPDV
	maxAbsIPDV := float64(4 * time.Millisecond.Nanoseconds())
	if jitter > maxAbsIPDV {
		t.Errorf("Jitter %v should be <= max abs IPDV %v", jitter, maxAbsIPDV)
	}
}

func TestCalculateRFC3550Jitter_EmptyInput(t *testing.T) {
	jitter := calculateRFC3550Jitter([]time.Duration{})
	if jitter != 0 {
		t.Errorf("Expected 0 jitter for empty input, got %v", jitter)
	}
}

func TestCalculateRFC3550Jitter_ConstantDelay(t *testing.T) {
	// Constant delay = zero IPDV = zero jitter
	ipdv := []time.Duration{0, 0, 0, 0, 0}
	jitter := calculateRFC3550Jitter(ipdv)

	if jitter != 0 {
		t.Errorf("Expected 0 jitter for constant delay, got %v", jitter)
	}
}

func TestCalculateRFC3550Jitter_ExponentialSmoothing(t *testing.T) {
	// Initial large IPDV followed by zeros
	// Jitter should decay exponentially
	ipdv := []time.Duration{
		16 * time.Millisecond, // Large initial value
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // 15 zeros
	}

	jitter := calculateRFC3550Jitter(ipdv)

	// After 16 iterations with zeros, jitter should decay significantly
	// J(n) = J(0) * (15/16)^n for zeros
	initialJitter := float64(16 * time.Millisecond.Nanoseconds()) / 16.0
	expectedDecay := initialJitter * math.Pow(15.0/16.0, 15)

	// Allow some tolerance
	tolerance := expectedDecay * 0.1
	if math.Abs(jitter-expectedDecay) > tolerance {
		t.Errorf("Expected decayed jitter around %v, got %v", expectedDecay, jitter)
	}
}

func TestCalculateForwardHops_ValidTTL(t *testing.T) {
	testCases := []struct {
		ttl      int
		expected int
	}{
		{255, 0},   // No hops
		{254, 1},   // 1 hop
		{245, 10},  // 10 hops
		{128, 127}, // Many hops
		{1, 254},   // Maximum hops
	}

	for _, tc := range testCases {
		hops := calculateForwardHops(tc.ttl)
		if hops != tc.expected {
			t.Errorf("TTL=%d: expected %d hops, got %d", tc.ttl, tc.expected, hops)
		}
	}
}

func TestCalculateForwardHops_InvalidTTL(t *testing.T) {
	testCases := []int{0, -1, 256, 1000}

	for _, ttl := range testCases {
		hops := calculateForwardHops(ttl)
		if hops != -1 {
			t.Errorf("TTL=%d: expected -1 (invalid), got %d", ttl, hops)
		}
	}
}

func TestCalculateReverseHops_LinuxDevice(t *testing.T) {
	// Linux typically uses initial TTL=64
	testCases := []struct {
		ttl      int
		expected int
	}{
		{64, 0},   // No hops
		{54, 10},  // 10 hops
		{32, 32},  // Half gone
	}

	for _, tc := range testCases {
		hops := calculateReverseHops(tc.ttl)
		if hops != tc.expected {
			t.Errorf("TTL=%d: expected %d hops, got %d", tc.ttl, tc.expected, hops)
		}
	}
}

func TestCalculateReverseHops_WindowsDevice(t *testing.T) {
	// Windows typically uses initial TTL=128
	testCases := []struct {
		ttl      int
		expected int
	}{
		{128, 0},  // No hops
		{118, 10}, // 10 hops
		{65, 63},  // Many hops
	}

	for _, tc := range testCases {
		hops := calculateReverseHops(tc.ttl)
		if hops != tc.expected {
			t.Errorf("TTL=%d: expected %d hops, got %d", tc.ttl, tc.expected, hops)
		}
	}
}

func TestCalculateReverseHops_NetworkDevice(t *testing.T) {
	// Network devices (Cisco, etc.) typically use initial TTL=255
	testCases := []struct {
		ttl      int
		expected int
	}{
		{255, 0},   // No hops
		{245, 10},  // 10 hops
		{129, 126}, // Many hops
	}

	for _, tc := range testCases {
		hops := calculateReverseHops(tc.ttl)
		if hops != tc.expected {
			t.Errorf("TTL=%d: expected %d hops, got %d", tc.ttl, tc.expected, hops)
		}
	}
}

func TestCalculateReverseHops_InvalidTTL(t *testing.T) {
	testCases := []int{0, -1}

	for _, ttl := range testCases {
		hops := calculateReverseHops(ttl)
		if hops != -1 {
			t.Errorf("TTL=%d: expected -1 (invalid), got %d", ttl, hops)
		}
	}
}

func TestCalculateReverseHops_InitialTTLEstimation(t *testing.T) {
	// Test the boundary conditions for initial TTL estimation
	testCases := []struct {
		ttl         int
		expectedTTL int // Estimated initial TTL
	}{
		{65, 128},  // >64 -> initial=128
		{64, 64},   // <=64 -> initial=64
		{129, 255}, // >128 -> initial=255
		{128, 128}, // <=128 but >64 -> initial=128
		{1, 64},    // Very low -> initial=64
	}

	for _, tc := range testCases {
		hops := calculateReverseHops(tc.ttl)
		expectedHops := tc.expectedTTL - tc.ttl
		if hops != expectedHops {
			t.Errorf("TTL=%d: expected initial TTL=%d, hops=%d, got hops=%d",
				tc.ttl, tc.expectedTTL, expectedHops, hops)
		}
	}
}
