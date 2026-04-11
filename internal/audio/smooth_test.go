package audio

import (
	"math"
	"sync"
	"testing"
)

func TestSmoothParam_Convergence(t *testing.T) {
	var sp SmoothParam
	sp.Init(0.0, 48000, 0.005)

	sp.Set(1.0)

	// After ~5ms (240 samples at 48kHz), should be within 63% of target
	for i := 0; i < 240; i++ {
		sp.Tick()
	}
	if sp.Current() < 0.5 {
		t.Errorf("after 240 samples, expected current > 0.5, got %f", sp.Current())
	}

	// After ~25ms (1200 samples), should be very close to target
	for i := 0; i < 960; i++ {
		sp.Tick()
	}
	if math.Abs(float64(sp.Current()-1.0)) > 0.01 {
		t.Errorf("after 1200 samples, expected current ≈ 1.0, got %f", sp.Current())
	}
}

func TestSmoothParam_Snap(t *testing.T) {
	var sp SmoothParam
	sp.Init(0.0, 48000, 0.005)
	sp.Set(0.75)
	sp.Snap()

	if sp.Current() != 0.75 {
		t.Errorf("after Snap, expected 0.75, got %f", sp.Current())
	}
}

func TestSmoothParam_ConcurrentSet(t *testing.T) {
	var sp SmoothParam
	sp.Init(0.0, 48000, 0.005)

	var wg sync.WaitGroup
	for g := 0; g < 10; g++ {
		wg.Add(1)
		go func(v float32) {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				sp.Set(v)
			}
		}(float32(g) / 10.0)
	}
	wg.Wait()

	// Just verify it doesn't panic and produces a valid float
	val := sp.Get()
	if math.IsNaN(float64(val)) || math.IsInf(float64(val), 0) {
		t.Errorf("concurrent Set produced invalid value: %f", val)
	}
}

func TestFadeEnvelope_FadeIn(t *testing.T) {
	fe := NewFadeEnvelope(48000, 0.01) // 480 samples fade

	if fe.State() != FadeSilent {
		t.Fatalf("expected FadeSilent, got %d", fe.State())
	}

	fe.TriggerFadeIn()
	if fe.State() != FadingIn {
		t.Fatalf("expected FadingIn, got %d", fe.State())
	}

	// Process a buffer smaller than fade length
	buf := make([][2]float32, 100)
	for i := range buf {
		buf[i] = [2]float32{1.0, 1.0}
	}
	fe.Process(buf)

	// First sample should be near 0, last should be ~100/480
	if buf[0][0] > 0.01 {
		t.Errorf("first sample should be near 0, got %f", buf[0][0])
	}
	expectedGain := float32(99) / float32(480)
	if math.Abs(float64(buf[99][0]-expectedGain)) > 0.01 {
		t.Errorf("last sample gain expected ~%f, got %f", expectedGain, buf[99][0])
	}

	// Process remaining to complete fade-in
	buf2 := make([][2]float32, 400)
	for i := range buf2 {
		buf2[i] = [2]float32{1.0, 1.0}
	}
	state := fe.Process(buf2)
	if state != FadeActive {
		t.Errorf("expected FadeActive after full fade-in, got %d", state)
	}
}

func TestFadeEnvelope_FadeOut(t *testing.T) {
	fe := NewFadeEnvelope(48000, 0.01) // 480 samples fade

	// Start active
	fe.TriggerFadeIn()
	buf := make([][2]float32, 500)
	for i := range buf {
		buf[i] = [2]float32{1.0, 1.0}
	}
	fe.Process(buf) // Complete fade-in
	if fe.State() != FadeActive {
		t.Fatalf("expected FadeActive, got %d", fe.State())
	}

	// Trigger fade-out
	fe.TriggerFadeOut()
	if fe.State() != FadingOut {
		t.Fatalf("expected FadingOut, got %d", fe.State())
	}

	buf2 := make([][2]float32, 500)
	for i := range buf2 {
		buf2[i] = [2]float32{1.0, 1.0}
	}
	state := fe.Process(buf2)
	if state != FadeSilent {
		t.Errorf("expected FadeSilent after full fade-out, got %d", state)
	}

	// Samples after fade-out should be zero
	if buf2[499][0] != 0 {
		t.Errorf("expected zero after fade-out, got %f", buf2[499][0])
	}
}

func TestFadeEnvelope_SilentZeroFills(t *testing.T) {
	fe := NewFadeEnvelope(48000, 0.01)

	buf := make([][2]float32, 10)
	for i := range buf {
		buf[i] = [2]float32{1.0, 1.0}
	}
	fe.Process(buf)

	for i, s := range buf {
		if s[0] != 0 || s[1] != 0 {
			t.Errorf("sample %d should be zero in silent state, got [%f, %f]", i, s[0], s[1])
		}
	}
}

func TestFadeEnvelope_InterruptFadeOut(t *testing.T) {
	fe := NewFadeEnvelope(48000, 0.01) // 480 samples

	// Fade in fully
	fe.TriggerFadeIn()
	buf := make([][2]float32, 500)
	for i := range buf {
		buf[i] = [2]float32{1.0, 1.0}
	}
	fe.Process(buf)

	// Start fade-out
	fe.TriggerFadeOut()
	buf2 := make([][2]float32, 240) // Half the fade
	for i := range buf2 {
		buf2[i] = [2]float32{1.0, 1.0}
	}
	fe.Process(buf2)

	// Interrupt with fade-in
	fe.TriggerFadeIn()
	if fe.State() != FadingIn {
		t.Errorf("expected FadingIn after interrupt, got %d", fe.State())
	}
}
