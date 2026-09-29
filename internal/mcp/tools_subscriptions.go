package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type subscriptionsIn struct {
	Offset int `json:"offset,omitempty" jsonschema:"index of the first subscription to return; default 0"`
}

type subscriptionsOut struct {
	Subscriptions []SingboxSubscription `json:"subscriptions"`
	Total         int                   `json:"total" jsonschema:"subscriptions on the router, ignoring paging"`
	Offset        int                   `json:"offset" jsonschema:"index of the first subscription returned"`
	Truncated     bool                  `json:"truncated" jsonschema:"true when subscriptions beyond this page remain — call again with a larger offset before concluding one is absent"`
}

// pageSubscriptions cuts one page. An offset past the end yields an empty
// page rather than an error: an agent walking the pages stops on it.
func pageSubscriptions(all []SingboxSubscription, offset int) subscriptionsOut {
	total := len(all)
	start := min(offset, total)
	end := min(start+MaxSubscriptionsInOutput, total)
	page := all[start:end:end]
	if page == nil {
		page = []SingboxSubscription{}
	}
	return subscriptionsOut{Subscriptions: page, Total: total, Offset: start, Truncated: end < total}
}

func registerSubscriptionTools(s *mcp.Server, d Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "list_singbox_subscriptions",
		Description: "sing-box subscriptions: remote or pasted server lists, each feeding one group of servers. Shows whether each is enabled, when it was last fetched and whether that fetch failed. " +
			"A subscription is not a tunnel: it never appears in list_tunnels or list_singbox_tunnels. " +
			"To see which server a subscription is using now, pass its groupTag to get_singbox_outbound. Subscription URLs are never returned — only the host.",
		Annotations: readOnly("List sing-box subscriptions"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in subscriptionsIn) (*mcp.CallToolResult, subscriptionsOut, error) {
		if in.Offset < 0 {
			return nil, subscriptionsOut{}, fmt.Errorf("offset must not be negative")
		}
		all, err := d.ListSingboxSubscriptions(ctx)
		if err != nil {
			return nil, subscriptionsOut{}, err
		}
		return nil, pageSubscriptions(all, in.Offset), nil
	})
}
