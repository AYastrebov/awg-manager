package mcp_test

import (
	mcpsrv "github.com/hoaxisr/awg-manager/internal/mcp"
	"github.com/hoaxisr/awg-manager/internal/mcp/mcptest"
	"strings"
	"testing"
)

// TestTools_ListSingboxTunnels — control_singbox умеет запустить и
// остановить движок, но ни один прокси внутри него агенту до сих пор
// виден не был: на «какие у меня прокси» ответить было нечем.
func TestTools_ListSingboxTunnels(t *testing.T) {
	s, _ := newTestSession(t)

	res, out := callTool(t, s, "list_singbox_tunnels", nil)
	if res.IsError {
		t.Fatal(toolText(res))
	}
	tunnels := out["tunnels"].([]any)
	if len(tunnels) != 2 {
		t.Fatalf("tunnels = %v", tunnels)
	}
	first := tunnels[0].(map[string]any)
	if first["tag"] != "vless-nl" || first["protocol"] != "vless" {
		t.Fatalf("tunnel = %v", first)
	}
	if first["server"] != "nl.example.net" || first["port"] != float64(443) {
		t.Fatalf("the endpoint is what tells two proxies apart: %v", first)
	}
	if first["running"] != true {
		t.Fatalf("running = %v", first["running"])
	}
	// A configured but dead proxy must be visible as such, not missing.
	second := tunnels[1].(map[string]any)
	if second["tag"] != "hy2-de" || second["running"] != false {
		t.Fatalf("tunnel = %v", second)
	}
}

// TestTools_SingboxDelayCheckSeparatesSilenceFromZero — Clash отвечает
// нулём и на «не ответил», и это ровно то значение, которое читается как
// «0 мс, отлично». Молчание обязано быть отдельным полем.
func TestTools_SingboxDelayCheck(t *testing.T) {
	s, _ := newTestSession(t)

	res, out := callTool(t, s, "singbox_delay_check", map[string]any{"tag": "vless-nl"})
	if res.IsError {
		t.Fatal(toolText(res))
	}
	if out["tag"] != "vless-nl" {
		t.Fatalf("tag = %v", out["tag"])
	}
	if out["reachable"] != true {
		t.Fatalf("reachable = %v", out["reachable"])
	}
	if out["delayMs"] != float64(120) {
		t.Fatalf("delayMs = %v", out["delayMs"])
	}

	// hy2-de times out: zero delay, and the tool must say it is silence.
	res, out = callTool(t, s, "singbox_delay_check", map[string]any{"tag": "hy2-de"})
	if res.IsError {
		t.Fatalf("no answer is a result, not a tool error: %s", toolText(res))
	}
	if out["reachable"] != false {
		t.Fatalf("reachable = %v, want false when the proxy did not answer", out["reachable"])
	}
	if out["delayMs"] != float64(0) {
		t.Fatalf("delayMs = %v", out["delayMs"])
	}
}

// TestTools_SingboxDelayCheckRejectsUnknownTag — тест задержки по
// несуществующему тегу просто не получит ответа и отчитался бы
// «недоступен». Опечатка в теге не должна выглядеть как упавший прокси.
func TestTools_SingboxDelayCheckRejectsUnknownTag(t *testing.T) {
	s, _ := newTestSession(t)

	if res, _ := callTool(t, s, "singbox_delay_check", map[string]any{"tag": "nope"}); !res.IsError {
		t.Error("an unknown tag must be a tool error, not an unreachable verdict")
	}
	if res, _ := callTool(t, s, "singbox_delay_check", map[string]any{}); !res.IsError {
		t.Error("a missing tag must be a tool error")
	}
}

// TestTools_SingboxDelayCheckBusyIsNotUnreachable — «проба уже идёт» и
// «не ответил» должны быть разными ответами: по второму агент скажет
// пользователю, что прокси упал.
func TestTools_SingboxDelayCheckBusyIsNotUnreachable(t *testing.T) {
	s, fake := newTestSession(t)
	fake.BusyDelays = map[string]bool{"vless-nl": true}

	res, out := callTool(t, s, "singbox_delay_check", map[string]any{"tag": "vless-nl"})
	if res.IsError {
		t.Fatalf("busy is a result, not an error: %s", toolText(res))
	}
	if out["busy"] != true {
		t.Fatalf("busy = %v", out["busy"])
	}
	if out["reachable"] != false {
		t.Fatalf("reachable = %v, want false with busy=true — no probe ran", out["reachable"])
	}
	if txt := strings.ToLower(toolText(res)); !strings.Contains(txt, "retry") {
		t.Fatalf("the text must tell the model to retry rather than conclude: %q", txt)
	}
}

// TestTools_SingboxDelayCheckKinds — проба принимала только теги из
// list_singbox_tunnels. Сервер подписки и группа получали «не найден»,
// хотя движок умеет мерить любой outbound.
func TestTools_SingboxDelayCheckKinds(t *testing.T) {
	s, _ := newTestSession(t)

	for tag, want := range map[string]string{
		"vless-nl":        "proxy",
		"sub-706dcf33-a1": "member",
		"sub-706dcf33":    "group",
	} {
		res, out := callTool(t, s, "singbox_delay_check", map[string]any{"tag": tag})
		if res.IsError {
			t.Fatalf("%s: %s", tag, toolText(res))
		}
		if out["kind"] != want || out["reachable"] != true {
			t.Fatalf("%s: out = %v, want kind %s", tag, out, want)
		}
	}

	// A group is measured through the member it routes through now, and
	// the answer must say which one that was.
	_, out := callTool(t, s, "singbox_delay_check", map[string]any{"tag": "sub-706dcf33"})
	if out["via"] != "sub-706dcf33-a1" {
		t.Fatalf("via = %v, want the active member", out["via"])
	}
	_, out = callTool(t, s, "singbox_delay_check", map[string]any{"tag": "vless-nl"})
	if _, has := out["via"]; has {
		t.Fatalf("via is for groups only: %v", out)
	}
}

// TestTools_SingboxDelayCheckSaysWhyItCannotProbe — исключённый сервер
// есть в подписке, но не в конфиге движка. «Не найден» отправил бы
// агента искать опечатку в теге, который ему только что выдали.
func TestTools_SingboxDelayCheckSaysWhyItCannotProbe(t *testing.T) {
	s, _ := newTestSession(t)

	res, _ := callTool(t, s, "singbox_delay_check", map[string]any{"tag": "sub-706dcf33-x9"})
	if !res.IsError {
		t.Fatal("an excluded server cannot be probed")
	}
	if txt := toolText(res); !strings.Contains(txt, "excluded") {
		t.Fatalf("the refusal must say why: %q", txt)
	}

	res, _ = callTool(t, s, "singbox_delay_check", map[string]any{"tag": "nope"})
	if !res.IsError {
		t.Fatal("an unknown tag must be an error, not a proxy that is down")
	}
	txt := toolText(res)
	for _, tool := range []string{"list_singbox_tunnels", "list_singbox_outbounds", "get_singbox_outbound"} {
		if !strings.Contains(txt, tool) {
			t.Errorf("the refusal must name %s: %q", tool, txt)
		}
	}

	for name, tag := range map[string]string{"a control character": "vless-nl\nx", "an over-long tag": strings.Repeat("a", 200)} {
		if res, _ := callTool(t, s, "singbox_delay_check", map[string]any{"tag": tag}); !res.IsError {
			t.Errorf("%s must be refused before Deps", name)
		}
	}
}

// TestTools_SingboxDelayCheckRefusesADraftOnlyGroup — группа из
// неприменённого черновика движку неизвестна. «Не отвечает» про неё —
// неправда: её никто не спрашивал.
func TestTools_SingboxDelayCheckRefusesADraftOnlyGroup(t *testing.T) {
	fake := mcptest.New()
	fake.RouterOutbounds = append(fake.RouterOutbounds, mcpsrv.SingboxOutbound{Tag: "draft-only", Type: "selector", Source: "user"})
	s := connect(t, mcpsrv.NewServer(fake, "test"))

	res, _ := callTool(t, s, "singbox_delay_check", map[string]any{"tag": "draft-only"})
	if !res.IsError {
		t.Fatal("a group the engine does not run must not be reported as unreachable")
	}
	if txt := toolText(res); !strings.Contains(txt, "not running it") || !strings.Contains(txt, "get_singbox_staging") {
		t.Fatalf("the refusal must say why and name the tool that shows the draft: %q", txt)
	}
}
