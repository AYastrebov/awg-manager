package mcp_test

import (
	"fmt"
	"testing"

	mcpsrv "github.com/hoaxisr/awg-manager/internal/mcp"
	"github.com/hoaxisr/awg-manager/internal/mcp/mcptest"
)

// TestTools_ListSingboxSubscriptions — пользователь завёл две подписки,
// list_tunnels и list_singbox_tunnels вернули пусто, и агент сообщил, что
// VPN на роутере нет. Подписка — отдельный объект, и у неё свой список.
func TestTools_ListSingboxSubscriptions(t *testing.T) {
	s, _ := newTestSession(t)

	res, out := callTool(t, s, "list_singbox_subscriptions", nil)
	if res.IsError {
		t.Fatal(toolText(res))
	}
	subs := out["subscriptions"].([]any)
	if len(subs) != 2 || out["total"] != float64(2) || out["truncated"] != false {
		t.Fatalf("out = %v", out)
	}
	first := subs[0].(map[string]any)
	if first["id"] != "706dcf33aabbccddeeff0011" || first["groupTag"] != "sub-706dcf33" || first["mode"] != "urltest" {
		t.Fatalf("subscription = %v", first)
	}
	if first["host"] != "sub.example.net" || first["source"] != "url" {
		t.Fatalf("subscription = %v", first)
	}
	// The store keeps an active server in every mode, and in urltest mode
	// it names a server the engine may not be using. The live answer is
	// in the group tools; a second field with the same name must not exist.
	for _, sub := range subs {
		if _, has := sub.(map[string]any)["activeMember"]; has {
			t.Fatalf("a subscription must not carry an active server: %v", sub)
		}
	}
}

// TestTools_ListSingboxSubscriptionsPages — обрезанный список без способа
// прочитать остаток — тупик: агент знает, что подписок больше, и не может
// до них добраться.
func TestTools_ListSingboxSubscriptionsPages(t *testing.T) {
	fake := mcptest.New()
	fake.Subscriptions = nil
	for i := range mcpsrv.MaxSubscriptionsInOutput + 5 {
		fake.Subscriptions = append(fake.Subscriptions, mcpsrv.SingboxSubscription{
			ID: fmt.Sprintf("%024x", i), Label: fmt.Sprintf("sub %03d", i), Source: "url", Mode: "selector", Enabled: true,
		})
	}
	s := connect(t, mcpsrv.NewServer(fake, "test"))

	_, out := callTool(t, s, "list_singbox_subscriptions", nil)
	if n := len(out["subscriptions"].([]any)); n != mcpsrv.MaxSubscriptionsInOutput {
		t.Fatalf("first page = %d entries", n)
	}
	if out["total"] != float64(mcpsrv.MaxSubscriptionsInOutput+5) || out["truncated"] != true {
		t.Fatalf("a capped page must carry the real total and say it is truncated: %v %v", out["total"], out["truncated"])
	}

	_, out = callTool(t, s, "list_singbox_subscriptions", map[string]any{"offset": mcpsrv.MaxSubscriptionsInOutput})
	if n := len(out["subscriptions"].([]any)); n != 5 || out["truncated"] != false {
		t.Fatalf("last page = %d entries, truncated=%v", n, out["truncated"])
	}

	// An agent walking the pages must be able to stop on an empty one.
	res, out := callTool(t, s, "list_singbox_subscriptions", map[string]any{"offset": 5000})
	if res.IsError {
		t.Fatalf("an offset past the end must be an empty page, not an error: %s", toolText(res))
	}
	if n := len(out["subscriptions"].([]any)); n != 0 {
		t.Fatalf("page past the end = %d entries", n)
	}

	if res, _ := callTool(t, s, "list_singbox_subscriptions", map[string]any{"offset": -1}); !res.IsError {
		t.Error("a negative offset must be a tool error")
	}
}
