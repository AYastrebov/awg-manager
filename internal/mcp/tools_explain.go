package mcp

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type explainIn struct {
	Target   string `json:"target" jsonschema:"a domain (e.g. \"www.youtube.com\") or a literal IPv4 address to explain"`
	ClientIP string `json:"clientIp,omitempty" jsonschema:"optional LAN device IPv4: include the device's own route in the answer"`
}

// explainDNSMatch is one domain list that covers the target, and the
// entry that made it match — an agent that can quote the entry can tell
// the user which line to edit.
type explainDNSMatch struct {
	RouteID      string `json:"routeId"`
	Name         string `json:"name"`
	Enabled      bool   `json:"enabled" jsonschema:"a disabled list matches but routes nothing"`
	MatchedEntry string `json:"matchedEntry" jsonschema:"the list entry the target matched, exactly as stored"`
	TunnelID     string `json:"tunnelId,omitempty"`
	TunnelName   string `json:"tunnelName,omitempty"`
}

// explainStaticMatch is one subnet list covering one of the target's
// addresses.
type explainStaticMatch struct {
	RouteID      string `json:"routeId"`
	Name         string `json:"name"`
	Enabled      bool   `json:"enabled"`
	MatchedEntry string `json:"matchedEntry" jsonschema:"the CIDR that contains the address"`
	MatchedIP    string `json:"matchedIp" jsonschema:"the address of the target that fell inside it"`
	TunnelID     string `json:"tunnelId,omitempty"`
	TunnelName   string `json:"tunnelName,omitempty"`
}

// explainUnevaluated names a list this tool could not decide about. It
// exists so an empty dnsMatches never silently means "no rule covers
// this": geosite:/geoip: tags are expanded on the router from data files
// MCP has no access to.
type explainUnevaluated struct {
	RouteID string `json:"routeId"`
	Name    string `json:"name"`
	Reason  string `json:"reason"`
}

type explainOut struct {
	Target      string   `json:"target"`
	IsIP        bool     `json:"isIp" jsonschema:"true when the target was given as a literal address"`
	ResolvedIPs []string `json:"resolvedIps" jsonschema:"IPv4 addresses the target resolves to right now; the router may resolve it differently later"`
	// ResolveError is set when the lookup failed. Domain matching still
	// ran; only the subnet comparison is missing.
	ResolveError         string               `json:"resolveError,omitempty"`
	DNSMatches           []explainDNSMatch    `json:"dnsMatches"`
	StaticMatches        []explainStaticMatch `json:"staticMatches"`
	ClientRoute          *ClientRoute         `json:"clientRoute,omitempty" jsonschema:"the device's own route, when clientIp was given"`
	UnevaluatedLists     []explainUnevaluated `json:"unevaluatedLists,omitempty" jsonschema:"lists this tool could not decide about — check them before telling the user nothing matches"`
	DefaultRouteTunnelID string               `json:"defaultRouteTunnelId,omitempty" jsonschema:"tunnel carrying the default route; traffic matching no rule goes here, or straight out the WAN when empty"`
	DefaultRouteTunnel   string               `json:"defaultRouteTunnelName,omitempty"`
	Note                 string               `json:"note" jsonschema:"how the matches above relate to each other in plain words"`
}

// normalizeDomain lowercases and strips the root dot so "WWW.Example.COM."
// and "www.example.com" compare equal.
func normalizeDomain(s string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), ".")
}

// domainCovers reports whether a routing-list entry covers target. The
// daemon's lists are suffix matches — an entry routes the domain and its
// subdomains — so "youtube.com" covers "www.youtube.com" but never
// "notyoutube.com".
func domainCovers(entry, target string) bool {
	entry = normalizeDomain(entry)
	if entry == "" {
		return false
	}
	return target == entry || strings.HasSuffix(target, "."+entry)
}

// unevaluatableEntry reports whether an entry is one this tool cannot
// decide locally: geosite:/geoip: tags expand from data files on the
// router. A CIDR entry is not unevaluatable — it is compared against the
// resolved addresses instead.
func unevaluatableEntry(entry string) bool {
	return strings.Contains(entry, ":") && !strings.Contains(entry, "/")
}

// matchDNSList decides one list against a target. It returns the matching
// entry, and separately whether the list held entries that could not be
// judged here.
func matchDNSList(entries []string, target string, ips []net.IP) (matched string, unevaluated bool) {
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if unevaluatableEntry(entry) {
			unevaluated = true
			continue
		}
		if _, subnet, err := net.ParseCIDR(entry); err == nil {
			for _, ip := range ips {
				if subnet.Contains(ip) {
					return entry, unevaluated
				}
			}
			continue
		}
		if target != "" && domainCovers(entry, target) {
			return entry, unevaluated
		}
	}
	return "", unevaluated
}

// matchSubnets returns the first CIDR in list containing one of ips.
func matchSubnets(subnets []string, ips []net.IP) (cidr string, hit string) {
	for _, s := range subnets {
		_, subnet, err := net.ParseCIDR(strings.TrimSpace(s))
		if err != nil {
			continue
		}
		for _, ip := range ips {
			if subnet.Contains(ip) {
				return strings.TrimSpace(s), ip.String()
			}
		}
	}
	return "", ""
}

// explainNote states in words how the matches relate. It deliberately
// stops short of naming a single winner when a device route and a list
// both apply: the device rule captures everything that device sends,
// while a list applies to that destination for every device, and which
// one wins is decided by the router's rule priorities, not here.
func explainNote(out *explainOut) string {
	var parts []string
	if out.ClientRoute != nil && out.ClientRoute.Enabled {
		parts = append(parts, fmt.Sprintf("the device %s has its own route through tunnel %s, which covers everything it sends, whatever the destination", out.ClientRoute.ClientIP, out.ClientRoute.TunnelID))
	}
	switch n := len(out.DNSMatches) + len(out.StaticMatches); {
	case n == 0 && len(out.UnevaluatedLists) > 0:
		parts = append(parts, "no list matched by name or subnet, but the lists under unevaluatedLists use geosite:/geoip: tags that only the router can expand — do not conclude the target is unrouted without checking them")
	case n == 0:
		parts = append(parts, "no routing list covers this target, so it follows the default route")
	case n > 1:
		parts = append(parts, "more than one list covers this target; the router applies them in its own order, so treat the list above as candidates rather than a decision")
	}
	if len(parts) == 0 {
		return "one routing list covers this target"
	}
	return strings.Join(parts, "; ")
}

func registerExplainTools(s *mcp.Server, d Deps) {
	mcp.AddTool(s, &mcp.Tool{
		Name: "explain_route",
		Description: "Explain which routing rules cover a domain or IP: the domain lists that match it (checked against every domain, not the truncated list view), " +
			"the subnet lists containing its addresses, the device's own route when clientIp is given, and the default route it falls back to. " +
			"Reports the candidates and how they relate; it does not simulate the router's rule priorities, and lists using geosite:/geoip: tags are reported as unevaluated rather than assumed not to match.",
		Annotations: readOnly("Explain route"),
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in explainIn) (*mcp.CallToolResult, explainOut, error) {
		target := normalizeDomain(in.Target)
		if target == "" {
			return nil, explainOut{}, fmt.Errorf("target is required (a domain or an IPv4 address)")
		}
		out := explainOut{Target: target, ResolvedIPs: []string{}, DNSMatches: []explainDNSMatch{}, StaticMatches: []explainStaticMatch{}}

		var ips []net.IP
		if ip := net.ParseIP(target); ip != nil && ip.To4() != nil {
			// A literal address: nothing to resolve, and no domain to
			// compare against list entries.
			out.IsIP = true
			ips = []net.IP{ip.To4()}
			out.ResolvedIPs = []string{ip.To4().String()}
			target = ""
		} else {
			addrs, err := d.ResolveDomain(ctx, target)
			if err != nil {
				// Subnet comparison is lost, domain matching is not — so
				// report the failure and carry on rather than fail the call.
				out.ResolveError = err.Error()
			}
			for _, a := range addrs {
				if ip := net.ParseIP(a); ip != nil && ip.To4() != nil {
					ips = append(ips, ip.To4())
					out.ResolvedIPs = append(out.ResolvedIPs, ip.To4().String())
				}
			}
		}

		if in.ClientIP != "" {
			ip := net.ParseIP(strings.TrimSpace(in.ClientIP))
			if ip == nil || ip.To4() == nil {
				return nil, explainOut{}, fmt.Errorf("clientIp %q is not a valid IPv4 address", in.ClientIP)
			}
			routes, err := d.ListClientRoutes(ctx)
			if err != nil {
				return nil, explainOut{}, err
			}
			want := ip.To4().String()
			for i := range routes {
				if routes[i].ClientIP == want {
					out.ClientRoute = &routes[i]
					break
				}
			}
		}

		tunnels, err := d.ListTunnels(ctx)
		if err != nil {
			return nil, explainOut{}, err
		}
		names := make(map[string]string, len(tunnels))
		for _, t := range tunnels {
			names[t.ID] = t.Name
			if t.DefaultRoute {
				out.DefaultRouteTunnelID, out.DefaultRouteTunnel = t.ID, t.Name
			}
		}

		lists, err := d.ListDNSRoutes(ctx)
		if err != nil {
			return nil, explainOut{}, err
		}
		for _, l := range lists {
			// The list view caps Domains, and matching against a truncated
			// list would answer "no rule covers this" for a rule that does.
			detail, err := d.GetDNSRoute(ctx, l.ID)
			if err != nil {
				out.UnevaluatedLists = append(out.UnevaluatedLists, explainUnevaluated{RouteID: l.ID, Name: l.Name, Reason: "could not be read: " + err.Error()})
				continue
			}
			entry, unevaluated := matchDNSList(detail.Domains, target, ips)
			if entry == "" {
				if sub, hit := matchSubnets(detail.Subnets, ips); sub != "" {
					entry, _ = sub, hit
				}
			}
			if entry != "" {
				m := explainDNSMatch{RouteID: detail.ID, Name: detail.Name, Enabled: detail.Enabled, MatchedEntry: entry}
				if len(detail.Routes) > 0 {
					m.TunnelID = detail.Routes[0].TunnelID
					m.TunnelName = names[m.TunnelID]
				}
				out.DNSMatches = append(out.DNSMatches, m)
				continue
			}
			if unevaluated {
				out.UnevaluatedLists = append(out.UnevaluatedLists, explainUnevaluated{
					RouteID: detail.ID, Name: detail.Name,
					Reason: "contains geosite:/geoip: tags, which only the router can expand",
				})
			}
		}

		statics, err := d.ListStaticRoutes(ctx)
		if err != nil {
			return nil, explainOut{}, err
		}
		for _, sr := range statics {
			cidr, hit := matchSubnets(sr.Subnets, ips)
			if cidr == "" {
				continue
			}
			out.StaticMatches = append(out.StaticMatches, explainStaticMatch{
				RouteID: sr.ID, Name: sr.Name, Enabled: sr.Enabled,
				MatchedEntry: cidr, MatchedIP: hit,
				TunnelID: sr.TunnelID, TunnelName: names[sr.TunnelID],
			})
		}

		out.Note = explainNote(&out)
		return nil, out, nil
	})
}
