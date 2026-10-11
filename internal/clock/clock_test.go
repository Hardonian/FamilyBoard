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
}

func TestMockIDGeneratorDeterministic(t *testing.T) {
	gen := clock.NewMockIDGenerator(0)

	id1 := gen.NewID("item")
	id2 := gen.NewID("item")

	if id1 != "item-00000001" {
		t.Errorf("expected item-00000001, got %s", id1)
	}
	if id2 != "item-00000002" {
		t.Errorf("expected item-00000002, got %s", id2)
	}
}
