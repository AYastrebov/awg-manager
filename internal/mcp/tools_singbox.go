package mcp

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type singboxTunnelsOut struct {
	Tunnels []SingboxTunnel `json:"tunnels"`
}

type singboxTagIn struct {
	Tag string `json:"tag" jsonschema:"a proxy tag from list_singbox_tunnels, a server tag from get_singbox_outbound, or a group tag from list_singbox_outbounds"`
}

type singboxIn struct {
	Action string `json:"action" jsonschema:"start|stop|restart"`
}

// maxSingboxTagLen bounds a tag before it reaches the engine. Tags the
// daemon generates are under 40 bytes; a user's group tag is a short name.
const maxSingboxTagLen = 128

// requireSingboxTag checks a tag's shape before Deps. The tag travels
// into a URL path of the engine's local API, so it is bounded and free of
// control characters; whether it exists is for Deps to say. listTool is
// named in the refusal so the agent knows where valid tags come from.
func requireSingboxTag(tag, listTool string) (string, error) {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return "", fmt.Errorf("tag is required (use %s)", listTool)
	}
	if len(tag) > maxSingboxTagLen {
		return "", fmt.Errorf("tag is longer than %d bytes", maxSingboxTagLen)
	}
	for _, r := range tag {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("tag must not contain control characters")
		}
	}
	return tag, nil
}

func registerSingboxTools(s *mcp.Server, d Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "list_singbox_tunnels",
		Description: "Proxies configured by hand inside sing-box (vless, hysteria2, naive) with their endpoint, local listen port and whether each one is actually up. " +
			"These are separate from the WireGuard/AmneziaWG tunnels in list_tunnels. Servers that come from a subscription are not listed here — see list_singbox_subscriptions. Passwords and uuids are not returned.",
		Annotations: readOnly("List sing-box proxies"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ empty) (*mcp.CallToolResult, singboxTunnelsOut, error) {
		list, err := d.ListSingboxTunnels(ctx)
		if list == nil {
			list = []SingboxTunnel{}
		}
		return nil, singboxTunnelsOut{Tunnels: list}, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name: "singbox_delay_check",
		Description: "Measure latency through one sing-box outbound: a proxy, a subscription server, or a group. " +
			"For a group the probe goes through the member it is routing through now (reported as via) — it does not test every member; read lastDelayMs in get_singbox_outbound for that. " +
			"reachable=false means it did not answer in time — a single silent check can be transient, so repeat it before telling the user it is down. " +
			"busy=true means nothing was measured because a probe was already running: retry in a few seconds. sing-box must be running.",
		Annotations: readOnly("Sing-box delay check"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in singboxTagIn) (*mcp.CallToolResult, SingboxDelay, error) {
		tag, err := requireSingboxTag(in.Tag, "list_singbox_tunnels, list_singbox_outbounds or get_singbox_outbound")
		if err != nil {
			return nil, SingboxDelay{}, err
		}
		out, err := d.CheckSingboxDelay(ctx, tag)
		if err != nil {
			return nil, SingboxDelay{}, err
		}
		if out.Busy {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{
				Text: "A probe for this outbound was already in progress, so nothing was measured; this says nothing about whether it works. Retry in a few seconds.",
			}}}, out, nil
		}
		return nil, out, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "control_singbox",
		Description: "Start, stop or restart the sing-box proxy engine and return its status. Install/uninstall are not available via MCP.",
		Annotations: safeWrite("Control sing-box", true),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in singboxIn) (*mcp.CallToolResult, SingboxStatus, error) {
		switch in.Action {
		case "start", "stop", "restart":
		default:
			return nil, SingboxStatus{}, fmt.Errorf("action must be start, stop or restart")
		}
		out, err := d.ControlSingbox(ctx, in.Action)
		return nil, out, err
	})
}
