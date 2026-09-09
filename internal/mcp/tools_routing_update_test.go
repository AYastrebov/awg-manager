package mcp_test

import (
	"testing"

	mcpsrv "github.com/hoaxisr/awg-manager/internal/mcp"
	"github.com/hoaxisr/awg-manager/internal/mcp/mcptest"
)

// TestTools_UpdateDNSRouteChangesOneFieldAtATime — раньше поправить один
// домен в списке можно было только через remove+add, а это теряло
// subnets, excludes, backend и подписки. Пропущенное поле обязано
// остаться прежним.
func TestTools_UpdateDNSRouteChangesOneFieldAtATime(t *testing.T) {
	s, _ := newTestSession(t)

	res, out := callTool(t, s, "update_dns_route", map[string]any{"routeId": "dl-1", "name": "Видео"})
	if res.IsError {
		t.Fatal(toolText(res))
	}
	if out["name"] != "Видео" {
		t.Fatalf("name = %v", out["name"])
	}
	if n := len(out["domains"].([]any)); n != 2 {
		t.Fatalf("a rename must not touch the domains, got %d", n)
	}

	res, out = callTool(t, s, "update_dns_route", map[string]any{"routeId": "dl-1", "domains": []string{"youtube.com", "ytimg.com", "nebula.tv"}})
	if res.IsError {
		t.Fatal(toolText(res))
	}
	if out["name"] != "Видео" {
		t.Fatalf("changing the domains must not touch the name, got %v", out["name"])
	}
	if n := len(out["domains"].([]any)); n != 3 {
		t.Fatalf("domains = %v", out["domains"])
	}

	res, out = callTool(t, s, "update_dns_route", map[string]any{"routeId": "dl-1", "tunnelId": "tn-2"})
	if res.IsError {
		t.Fatal(toolText(res))
	}
	routes := out["routes"].([]any)
	if len(routes) != 1 || routes[0].(map[string]any)["tunnelId"] != "tn-2" {
		t.Fatalf("routes = %v", routes)
	}
	if n := len(out["domains"].([]any)); n != 3 {
		t.Fatalf("re-pointing the tunnel must not touch the domains, got %d", n)
	}
}

func TestTools_UpdateDNSRouteRejectsNonsense(t *testing.T) {
	s, _ := newTestSession(t)

	// Nothing to change is a mistake worth reporting: a silent no-op would
	// let an agent tell the user it renamed a list it never touched.
	if res, _ := callTool(t, s, "update_dns_route", map[string]any{"routeId": "dl-1"}); !res.IsError {
		t.Error("an update with no fields must be a tool error")
	}
	if res, _ := callTool(t, s, "update_dns_route", map[string]any{"routeId": "nope", "name": "x"}); !res.IsError {
		t.Error("unknown routeId must be a tool error")
	}
	if res, _ := callTool(t, s, "update_dns_route", map[string]any{"name": "x"}); !res.IsError {
		t.Error("missing routeId must be a tool error")
	}
	if res, _ := callTool(t, s, "update_dns_route", map[string]any{"routeId": "dl-1", "domains": []string{"  "}}); !res.IsError {
		t.Error("a blank domain must be a tool error")
	}
	if res, _ := callTool(t, s, "update_dns_route", map[string]any{"routeId": "dl-1", "tunnelId": "../settings"}); !res.IsError {
		t.Error("a traversal tunnelId must be rejected before Deps")
	}
	if res, _ := callTool(t, s, "update_dns_route", map[string]any{"routeId": "dl-1", "domains": []string{}}); !res.IsError {
		t.Error("an empty domain list must be a tool error, not a way to empty the list")
	}
}

// TestTools_UpdateDNSRouteWarnsWhenItDropsExtraTargets — у списка может
// быть несколько целей маршрутизации, а tunnelId задаёт ровно одну.
// Молча потерять остальные нельзя: пользователь должен узнать.
func TestTools_UpdateDNSRouteWarnsWhenItDropsExtraTargets(t *testing.T) {
	fake := mcptest.New()
	fake.DNSRoutes = append(fake.DNSRoutes, mcpsrv.DNSRouteDetail{
		ID: "dl-multi", Name: "Split", Enabled: true,
		Domains: []string{"a.example"}, ManualDomains: []string{"a.example"},
		Routes: []mcpsrv.RouteTarget{{TunnelID: "tn-1"}, {TunnelID: "tn-2"}},
	})
	s := connect(t, mcpsrv.NewServer(fake, "test"))

	res, out := callTool(t, s, "update_dns_route", map[string]any{"routeId": "dl-multi", "tunnelId": "tn-2"})
	if res.IsError {
		t.Fatal(toolText(res))
	}
	warnings, _ := out["warnings"].([]any)
	if len(warnings) == 0 {
		t.Fatalf("replacing 2 route targets with 1 must warn, got %v", out)
	}

	// A rename on the same list changes no targets, so it must stay quiet.
	_, out = callTool(t, s, "update_dns_route", map[string]any{"routeId": "dl-multi", "name": "Split 2"})
	if w, _ := out["warnings"].([]any); len(w) != 0 {
		t.Fatalf("a rename must not warn about targets: %v", w)
	}
}
