package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// readOnlyTools names every tool that changes nothing on the router. It
// is written out by hand because the SDK does not expose its registry,
// so the set and the tools' own readOnlyHint annotations are two separate
// claims about the same thing; TestScope_ReadOnlySetMatchesTheAnnotations
// holds them together. Anything missing from this set is refused for a
// read-only key rather than allowed — a forgotten entry must cost a
// puzzling refusal, never a silent hole.
var readOnlyTools = map[string]bool{
	"get_system_status":     true,
	"get_logs":              true,
	"test_connectivity":     true,
	"check_ip":              true,
	"get_monitoring_matrix": true,
	"list_tunnels":          true,
	"get_tunnel":            true,
	"export_tunnel_config":  true,
	"list_dns_routes":       true,
	"get_dns_route":         true,
	"list_static_routes":    true,
	"list_client_routes":    true,
	"list_access_policies":  true,
	"list_devices":          true,
	"explain_route":         true,
	"list_managed_servers":  true,
	"list_singbox_tunnels":  true,
	"singbox_delay_check":   true,
}

// IsReadOnlyTool reports whether name is known to change nothing. An
// unknown name is not read-only.
func IsReadOnlyTool(name string) bool { return readOnlyTools[name] }

// RequireWriteScope refuses tools/call for anything that writes when the
// authenticated key is read-only. It sits in front of the tool handler,
// so a refused call never reaches the router: reporting "not allowed"
// after the change had already been applied would be the worst outcome
// of the three.
//
// A request with no key in context is left alone. Authentication is
// KeyMiddleware's job, and it rejects those before the handler is ever
// reached; treating "no key" as read-only here would instead break the
// dev server (cmd/mcp-dev), which runs the same tools with no auth at all.
func RequireWriteScope() mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method != "tools/call" {
				return next(ctx, method, req)
			}
			key, ok := KeyFromContext(ctx)
			if !ok || !key.ReadOnly {
				return next(ctx, method, req)
			}
			call, isCall := req.(*mcp.CallToolRequest)
			if !isCall {
				// The method says tools/call but the payload is not one.
				// Fail closed: this is the branch an attacker would want.
				return nil, fmt.Errorf("this MCP key is read-only")
			}
			name := call.Params.Name
			if IsReadOnlyTool(name) {
				return next(ctx, method, req)
			}
			// Returned as a tool result, not a transport error, so the
			// model reads the reason and can tell the user which key to
			// swap rather than seeing an opaque protocol failure.
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(
					"Tool %q changes the router's configuration, and this MCP key is read-only. "+
						"Nothing was changed. Ask the user for a full-access key, or make the change in the web interface.", name)}},
			}, nil
		}
	}
}
