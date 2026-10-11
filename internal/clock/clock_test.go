package clock_test

import (
	"testing"
	"time"

	"github.com/Hardonian/FamilyBoard/internal/clock"
)

func TestMockClockAdvancement(t *testing.T) {
	start := time.Date(2026, 10, 11, 8, 0, 0, 0, time.UTC)
	clk := clock.NewMockClock(start)

	if clk.Now() != start {
		t.Fatalf("expected initial time %v, got %v", start, clk.Now())
	}

	clk.Add(15 * time.Minute)
	expected := time.Date(2026, 10, 11, 8, 15, 0, 0, time.UTC)
	if clk.Now() != expected {
		t.Fatalf("expected advanced time %v, got %v", expected, clk.Now())
	}

	if clk.Since(start) != 15*time.Minute {
		t.Fatalf("expected Since to be 15m, got %v", clk.Since(start))
	}

	if clk.Until(expected.Add(5*time.Minute)) != 5*time.Minute {
		t.Fatalf("expected Until to be 5m")
	}

	clk.Set(start)
	if clk.Now() != start {
		t.Fatalf("expected Set to reset time")
	}

	clk.Sleep(10 * time.Minute)
	if clk.Now() != start.Add(10*time.Minute) {
		t.Fatalf("expected Sleep to advance mock clock")
	}

	select {
	case <-clk.After(time.Minute):
	default:
		t.Fatalf("expected channel receive from mock After")
	}

	defaultClk := clock.NewMockClock(time.Time{})
	if defaultClk.Now().IsZero() {
		t.Fatalf("expected non-zero default time")
	}
}

func TestRealClock(t *testing.T) {
	rc := clock.NewRealClock()
	now := rc.Now()
	if now.IsZero() {
		t.Fatalf("expected non-zero now")
	}
	if rc.Since(now) < 0 {
		t.Fatalf("expected non-negative Since")
	}
	future := now.Add(100 * time.Millisecond)
	if rc.Until(future) < 0 {
		t.Fatalf("expected positive Until")
	}
	rc.Sleep(1 * time.Millisecond)
	ch := rc.After(1 * time.Millisecond)
	<-ch
}

func TestMockIDGeneratorDeterministic(t *testing.T) {
	gen := clock.NewMockIDGenerator(0)

	id1 := gen.NewID("item")
	id2 := gen.NewID("item")
	id3 := gen.NewID("")

	if id1 != "item-00000001" {
		t.Errorf("expected item-00000001, got %s", id1)
	}
	if id2 != "item-00000002" {
		t.Errorf("expected item-00000002, got %s", id2)
	}
	if id3 != "00000003" {
		t.Errorf("expected 00000003, got %s", id3)
	}
}

func TestRealIDGenerator(t *testing.T) {
	gen := clock.NewRealIDGenerator()
	id1 := gen.NewID("test")
	id2 := gen.NewID("")
	if len(id1) < 5 || len(id2) < 5 {
		t.Fatalf("expected generated IDs to be non-empty")
	}
	if id1 == id2 {
		t.Fatalf("expected distinct random IDs")
	}
}
