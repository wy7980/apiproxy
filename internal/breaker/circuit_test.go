package breaker

import "testing"

func TestBreakerDefaultClosed(t *testing.T) {
	b := New()
	if !b.Allow("p1") {
		t.Fatal("Allow(p1) = false, want true for default")
	}
	if b.State("p1") != Closed {
		t.Fatalf("State(p1) = %v, want Closed", b.State("p1"))
	}
}

func TestBreakerOpenDenies(t *testing.T) {
	b := New()
	b.Set("p1", Open)
	if b.Allow("p1") {
		t.Fatal("Allow(p1) = true, want false when open")
	}
	if b.State("p1") != Open {
		t.Fatalf("State(p1) = %v, want Open", b.State("p1"))
	}
}

func TestBreakerHalfOpenAllows(t *testing.T) {
	b := New()
	b.Set("p1", HalfOpen)
	if !b.Allow("p1") {
		t.Fatal("Allow(p1) = false, want true when half-open")
	}
}
