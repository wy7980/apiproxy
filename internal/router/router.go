package router

import (
	"fmt"

	"github.com/wangyong/apiproxy/internal/config"
)

type Router struct {
	routes map[string]config.Route
}

func New(routes map[string]config.Route) *Router {
	return &Router{routes: routes}
}

func (r *Router) Resolve(model string) (*ResolvedRoute, error) {
	route, ok := r.routes[model]
	if !ok {
		return nil, fmt.Errorf("route %q not found", model)
	}
	return &ResolvedRoute{
		Name:      model,
		Strategy:  route.Strategy,
		Fallback:  route.Fallback,
		Providers: route.Providers,
	}, nil
}

type ResolvedRoute struct {
	Name      string
	Strategy  string
	Fallback  config.FallbackConfig
	Providers []config.RouteTarget
}

func (r *ResolvedRoute) OrderedTargets() []config.RouteTarget {
	out := make([]config.RouteTarget, len(r.Providers))
	copy(out, r.Providers)
	return out
}
