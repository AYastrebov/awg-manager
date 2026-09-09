package mcp_test

import (
	"testing"

	mcpsrv "github.com/hoaxisr/awg-manager/internal/mcp"
)

// TestTools_SetDNSRouteEnabled — до этого выключить список можно было
// только через remove_dns_route, то есть безвозвратно. Переключатель
// обратим, поэтому он и есть правильный ответ на «убери это пока».
func TestTools_SetDNSRouteEnabled(t *testing.T) {
	s, fake := newTestSession(t)

	res, out := callTool(t, s, "set_dns_route_enabled", map[string]any{"routeId": "dl-1", "enabled": false})
	if res.IsError {
		t.Fatal(toolText(res))
	}
	if out["enabled"] != false {
		t.Fatalf("enabled = %v, want the updated record to say false", out["enabled"])
	}
	if out["id"] != "dl-1" {
		t.Fatalf("id = %v", out["id"])
	}
	// The list is disabled, not deleted: it must still be there to turn on.
	list, _ := fake.ListDNSRoutes(t.Context())
	if len(list) != 1 || list[0].Enabled {
		t.Fatalf("list after disable = %+v", list)
	}

	_, out = callTool(t, s, "set_dns_route_enabled", map[string]any{"routeId": "dl-1", "enabled": true})
	if out["enabled"] != true {
		t.Fatalf("re-enable = %v", out)
	}

	if res, _ = callTool(t, s, "set_dns_route_enabled", map[string]any{"routeId": "nope", "enabled": true}); !res.IsError {
		t.Error("unknown routeId must be a tool error")
	}
	if res, _ = callTool(t, s, "set_dns_route_enabled", map[string]any{"enabled": true}); !res.IsError {
		t.Error("missing routeId must be a tool error")
	}
}

// TestTools_SetEnabledToolsAreReversibleWrites — хост решает, спрашивать
// ли пользователя, по destructiveHint. Переключатель обратим, и пометить
// его разрушающим значило бы приучать соглашаться на настоящие удаления.
func TestTools_SetEnabledToolsAreReversibleWrites(t *testing.T) {
	s, _ := newTestSession(t)
	tools, err := s.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, tool := range tools.Tools {
		switch tool.Name {
		case "set_dns_route_enabled":
			seen++
			a := tool.Annotations
			if a == nil || a.ReadOnlyHint || a.DestructiveHint == nil || *a.DestructiveHint || !a.IdempotentHint {
				t.Errorf("%s: annotations = %+v, want a non-destructive idempotent write", tool.Name, a)
			}
		}
	}
	if seen != 1 {
		t.Fatalf("saw %d of the expected toggle tools", seen)
	}
	_ = mcpsrv.MaxDomainsInOutput
}
