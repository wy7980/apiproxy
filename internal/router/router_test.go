package router

import (
	"testing"

	"github.com/wangyong/apiproxy/internal/config"
)

func TestResolve(t *testing.T) {
	r := New(map[string]config.Route{
		"chat": {
			Strategy: "priority",
			Providers: []config.RouteTarget{{Provider: "p1", Model: "m1"}},
		},
	})

	got, err := r.Resolve("chat")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got.Name != "chat" || got.Strategy != "priority" || len(got.Providers) != 1 {
		t.Fatalf("Resolve() = %+v", got)
	}
}

func TestResolveNotFound(t *testing.T) {
	r := New(nil)
	if _, err := r.Resolve("missing"); err == nil {
		t.Fatal("Resolve() error = nil, want error")
	}
}

func TestOrderedTargetsReturnsCopy(t *testing.T) {
	r := &ResolvedRoute{Providers: []config.RouteTarget{{Provider: "p1", Model: "m1"}}}
	got := r.OrderedTargets()
	got[0].Provider = "changed"
	if r.Providers[0].Provider != "p1" {
		t.Fatal("OrderedTargets() did not return a copy")
	}
}
