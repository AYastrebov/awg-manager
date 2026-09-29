package localdeps

import (
	"context"
	"fmt"
	"strings"
	"time"

	mcpsrv "github.com/hoaxisr/awg-manager/internal/mcp"
	"github.com/hoaxisr/awg-manager/internal/singbox"
	"github.com/hoaxisr/awg-manager/internal/singbox/router"
	"github.com/hoaxisr/awg-manager/internal/singbox/subscription"
)

// singboxSubscription maps a subscription field by field. Nothing that
// can carry the provider's token is read here: not the URL beyond its
// host, not the headers, not the pasted body, not the file path.
func singboxSubscription(s *subscription.Subscription) mcpsrv.SingboxSubscription {
	out := mcpsrv.SingboxSubscription{
		ID:              s.ID,
		Label:           sanitizeLabel(s.Label),
		Source:          "url",
		Host:            hostOf(s.URL),
		Enabled:         s.Enabled,
		Mode:            string(s.EffectiveMode()),
		GroupTag:        s.SelectorTag,
		MemberCount:     len(s.MemberTags),
		ExcludedCount:   len(s.ExcludedTags),
		OrphanCount:     len(s.OrphanTags),
		RefreshHours:    s.RefreshHours,
		LastFetchFailed: s.LastError != "",
		LastErrorKind:   lastErrorKind(s),
	}
	switch {
	case s.IsInline():
		out.Source = "inline"
	case s.IsFile():
		out.Source = "file"
	}
	if !s.LastFetched.IsZero() {
		out.LastFetched = s.LastFetched.UTC().Format(time.RFC3339)
	}
	return out
}

// ListSingboxSubscriptions returns every subscription in the store's own
// order (label, then id), so that an offset is stable between calls.
func (l *Local) ListSingboxSubscriptions(context.Context) ([]mcpsrv.SingboxSubscription, error) {
	if l.c.Subscriptions == nil {
		return nil, errUnavailable("sing-box subscriptions")
	}
	list := l.c.Subscriptions.List()
	out := make([]mcpsrv.SingboxSubscription, 0, len(list))
	for i := range list {
		out = append(out, singboxSubscription(&list[i]))
	}
	return out, nil
}

// lastErrorKind reduces the stored error to one word. The text itself
// never crosses the boundary, and masking it would not be enough: it is
// built from arbitrary errors, so it can quote the subscription URL, the
// path of a file subscription, and — through the parser — fragments of
// the list itself, a server's uuid included (a failed url.Parse embeds
// the whole share link).
//
// The two markers are the daemon's own wording in subscription.Service
// (refreshLockedOpts, "len(parts.Valid) == 0"). If that wording changes,
// the answer degrades to the source-based kind below; it never leaks.
func lastErrorKind(s *subscription.Subscription) string {
	switch {
	case s.LastError == "":
		return ""
	case strings.Contains(s.LastError, "подписка пуста"):
		return "empty"
	case strings.Contains(s.LastError, "ни одной валидной ссылки"):
		return "parse"
	case s.IsFile():
		return "file"
	case s.IsInline():
		// A pasted list is never fetched: what can fail is parsing it.
		return "parse"
	case s.URL != "":
		return "network"
	}
	return "other"
}

// sbIndex is one consistent read of what sing-box is configured with,
// keyed the way the group tools look things up.
type sbIndex struct {
	groups      map[string]router.CompositeOutboundView
	order       []string          // group tags, in configuration order
	subByGroup  map[string]string // a subscription's own group tag → its id
	aggByTag    map[string]subscription.AggregateGroup
	member      map[string]subscription.MemberInfo // subscription server tag → what it is
	isMember    map[string]bool                    // tags that are outbounds of a subscription
	notOutbound map[string]string                  // tag → why it cannot be used
	proxies     map[string]singbox.TunnelInfo      // hand-configured proxies
}

// singboxIndex reads the router, the subscriptions and the operator.
// Each is optional: a daemon without subscriptions still lists its
// groups, and the fields that would link them stay empty.
func (l *Local) singboxIndex(ctx context.Context) (sbIndex, error) {
	idx := sbIndex{
		groups:      map[string]router.CompositeOutboundView{},
		subByGroup:  map[string]string{},
		aggByTag:    map[string]subscription.AggregateGroup{},
		member:      map[string]subscription.MemberInfo{},
		isMember:    map[string]bool{},
		notOutbound: map[string]string{},
		proxies:     map[string]singbox.TunnelInfo{},
	}
	if l.c.Router != nil {
		list, err := l.c.Router.ListCompositeOutbounds(ctx)
		if err != nil {
			return idx, err
		}
		for _, o := range list {
			idx.groups[o.Tag] = o
			idx.order = append(idx.order, o.Tag)
		}
	}
	if l.c.Subscriptions != nil {
		for _, s := range l.c.Subscriptions.List() {
			name := sanitizeLabel(s.Label)
			idx.subByGroup[s.SelectorTag] = s.ID
			// A server orphaned by the last refresh stays an outbound until
			// the user deletes it (subscription.Service.DeleteOrphans).
			for _, tag := range s.MemberTags {
				idx.isMember[tag] = true
			}
			for _, tag := range s.OrphanTags {
				idx.isMember[tag] = true
			}
			for _, m := range s.Members {
				idx.member[m.Tag] = m
			}
			for _, tag := range s.ExcludedTags {
				idx.notOutbound[tag] = fmt.Sprintf("it is excluded from subscription %q by the user, so it is not an outbound", name)
			}
			for _, m := range s.FilteredMembers {
				idx.notOutbound[m.Tag] = fmt.Sprintf("it is hidden by the filter of subscription %q, so it is not an outbound", name)
			}
		}
		for _, g := range l.c.Subscriptions.ListGroups() {
			idx.aggByTag[g.Tag] = g
		}
	}
	if l.c.Singbox != nil {
		// A failure here costs only the proxies' descriptions.
		if list, err := l.c.Singbox.ListTunnels(ctx); err == nil {
			for _, t := range list {
				idx.proxies[t.Tag] = t
			}
		}
	}
	return idx, nil
}

// kindOf classifies a tag from configuration, never from the engine, so
// that a mistyped tag gets a precise answer while sing-box is down. The
// first match wins: group, member, proxy.
func (idx sbIndex) kindOf(tag string) string {
	switch {
	case hasKey(idx.groups, tag):
		return "group"
	case idx.isMember[tag]:
		return "member"
	case hasKey(idx.proxies, tag):
		return "proxy"
	}
	return ""
}

func hasKey[V any](m map[string]V, k string) bool { _, ok := m[k]; return ok }

// clashProxies reads the engine once. Any failure means the present is
// unknown; it is never turned into an empty answer.
func (l *Local) clashProxies() (map[string]singbox.ClashProxy, bool) {
	if l.c.Clash == nil {
		return nil, false
	}
	proxies, err := l.c.Clash.GetProxies()
	if err != nil {
		return nil, false
	}
	return proxies, true
}

// outbound assembles one group's summary.
//
// runtimeKnown is per group: the engine answered AND has an entry for this
// group. The router lists groups from the draft when one exists
// (orchestrator.LoadEffective), so a group can be configured and unknown to
// the engine; staged marks that, and a member list that differs from the
// engine's.
func (idx sbIndex) outbound(o router.CompositeOutboundView, proxies map[string]singbox.ClashProxy, engineAnswered bool) mcpsrv.SingboxOutbound {
	gp, inEngine := proxies[o.Tag]
	runtimeKnown := engineAnswered && inEngine
	out := mcpsrv.SingboxOutbound{
		Tag: o.Tag, Type: o.Type, Source: o.Source,
		MemberCount:  len(o.Outbounds),
		RuntimeKnown: runtimeKnown,
		Staged:       engineAnswered && (!inEngine || !sameTags(gp.All, o.Outbounds)),
	}
	if id, ok := idx.subByGroup[o.Tag]; ok {
		out.SubscriptionID = id
	}
	if g, ok := idx.aggByTag[o.Tag]; ok {
		out.AggregateOf = append([]string(nil), g.UseSubscriptionIDs...)
	}
	if runtimeKnown {
		out.ActiveMember = gp.Now
		out.ActiveMemberLabel = sanitizeLabel(idx.member[out.ActiveMember].Label)
	}
	return out
}

// sameTags compares two tag lists as sets: the order of members is not a
// difference.
func sameTags(a, b []string) bool {
	set := make(map[string]struct{}, len(a))
	for _, t := range a {
		set[t] = struct{}{}
	}
	other := make(map[string]struct{}, len(b))
	for _, t := range b {
		other[t] = struct{}{}
	}
	if len(set) != len(other) {
		return false
	}
	for t := range other {
		if _, ok := set[t]; !ok {
			return false
		}
	}
	return true
}

// ListSingboxOutbounds lists every group. The engine is read once for
// the whole list.
func (l *Local) ListSingboxOutbounds(ctx context.Context) ([]mcpsrv.SingboxOutbound, error) {
	if l.c.Router == nil {
		return nil, errUnavailable("sing-box router")
	}
	idx, err := l.singboxIndex(ctx)
	if err != nil {
		return nil, err
	}
	proxies, known := l.clashProxies()
	out := make([]mcpsrv.SingboxOutbound, 0, len(idx.order))
	for _, tag := range idx.order {
		out = append(out, idx.outbound(idx.groups[tag], proxies, known))
	}
	return out, nil
}

// groupMember describes one member of a group.
func (idx sbIndex) groupMember(tag, active string, proxies map[string]singbox.ClashProxy, runtimeKnown bool) mcpsrv.SingboxGroupMember {
	m := mcpsrv.SingboxGroupMember{Tag: tag, Kind: idx.kindOf(tag)}
	switch m.Kind {
	case "member":
		info := idx.member[tag]
		m.Label = sanitizeLabel(info.Label)
		m.Protocol, m.Server, m.Port = info.Protocol, info.Server, int(info.Port)
		m.Transport, m.Security = info.Transport, info.Security
	case "proxy":
		t := idx.proxies[tag]
		m.Protocol, m.Server, m.Port = t.Protocol, t.Server, t.Port
		m.Transport, m.Security = t.Transport, t.Security
	case "":
		// In the group's configuration and nowhere else we can see: a
		// built-in such as direct, or an AWG outbound. Calling it a proxy
		// would send the agent to list_singbox_tunnels, where it is not.
		m.Kind = "other"
	}
	if !runtimeKnown {
		return m
	}
	isActive := tag == active
	m.Active = &isActive
	// The engine appends to history; the last entry is the latest test.
	// A delay of 0 there is a test that got no answer.
	if h := proxies[tag].History; len(h) > 0 {
		m.DelayKnown = true
		if d := h[len(h)-1].Delay; d > 0 {
			m.LastDelayMs = &d
		}
	}
	return m
}

// GetSingboxOutbound returns one group with all its members.
func (l *Local) GetSingboxOutbound(ctx context.Context, tag string) (mcpsrv.SingboxOutboundDetail, error) {
	if l.c.Router == nil {
		return mcpsrv.SingboxOutboundDetail{}, errUnavailable("sing-box router")
	}
	idx, err := l.singboxIndex(ctx)
	if err != nil {
		return mcpsrv.SingboxOutboundDetail{}, err
	}
	o, ok := idx.groups[tag]
	if !ok {
		switch idx.kindOf(tag) {
		case "member":
			return mcpsrv.SingboxOutboundDetail{}, fmt.Errorf("%q is a single server, not a group (groups are in list_singbox_outbounds)", tag)
		case "proxy":
			return mcpsrv.SingboxOutboundDetail{}, fmt.Errorf("%q is a single proxy, not a group (groups are in list_singbox_outbounds)", tag)
		}
		return mcpsrv.SingboxOutboundDetail{}, fmt.Errorf("sing-box group %q not found (use list_singbox_outbounds)", tag)
	}
	proxies, known := l.clashProxies()
	out := mcpsrv.SingboxOutboundDetail{
		SingboxOutbound: idx.outbound(o, proxies, known),
		Members:         make([]mcpsrv.SingboxGroupMember, 0, len(o.Outbounds)),
	}
	for _, memberTag := range o.Outbounds {
		out.Members = append(out.Members, idx.groupMember(memberTag, out.ActiveMember, proxies, out.RuntimeKnown))
	}
	return out, nil
}
