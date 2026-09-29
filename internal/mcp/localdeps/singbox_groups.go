package localdeps

import (
	"context"
	"errors"
	"fmt"
	"slices"
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

// CheckSingboxDelay probes one outbound. The tag is classified from
// configuration first: the delay test itself answers "no response" for a
// tag that does not exist, so a typo would otherwise be reported as an
// outbound that is down.
func (l *Local) CheckSingboxDelay(ctx context.Context, tag string) (mcpsrv.SingboxDelay, error) {
	if l.c.Singbox == nil {
		return mcpsrv.SingboxDelay{}, errUnavailable("sing-box")
	}
	idx, err := l.singboxIndex(ctx)
	if err != nil {
		return mcpsrv.SingboxDelay{}, err
	}
	kind := idx.kindOf(tag)
	if kind == "" {
		if why, ok := idx.notOutbound[tag]; ok {
			return mcpsrv.SingboxDelay{}, fmt.Errorf("%q cannot be probed: %s", tag, why)
		}
		return mcpsrv.SingboxDelay{}, fmt.Errorf("sing-box outbound %q not found (proxies are in list_singbox_tunnels, groups in list_singbox_outbounds, a group's servers in get_singbox_outbound)", tag)
	}
	// Configuration may be an unapplied draft (orchestrator.LoadEffective),
	// and the prober answers 0 for an outbound the engine does not have —
	// the same 0 it answers for one that is down. So an outbound the engine
	// answered about and does not know is refused, not probed. When the
	// engine does not answer at all, the probe goes ahead and says so.
	if proxies, answered := l.clashProxies(); answered {
		if _, running := proxies[tag]; !running {
			return mcpsrv.SingboxDelay{}, fmt.Errorf("%q is configured but sing-box is not running it: it comes from changes that are not applied yet (get_singbox_staging), or sing-box has not reloaded. Nothing was measured, and this says nothing about whether it works", tag)
		}
	}
	ms, err := l.c.Singbox.CheckDelay(ctx, tag)
	if errors.Is(err, singbox.ErrProbeInFlight) {
		// The periodic sweep shares the prober and holds a slow outbound's
		// tag for several seconds; nothing was measured here, so neither
		// verdict applies.
		return mcpsrv.SingboxDelay{Tag: tag, Kind: kind, Busy: true}, nil
	}
	if err != nil {
		return mcpsrv.SingboxDelay{}, err
	}
	// CheckOne normalises a timeout to 0 ms, so 0 means silence — not a
	// round trip that took no time.
	out := mcpsrv.SingboxDelay{Tag: tag, Kind: kind, Reachable: ms > 0, DelayMs: ms}
	if kind == "group" {
		// Read after the probe: a urltest group may have just re-chosen.
		if proxies, answered := l.clashProxies(); answered {
			out.Via = proxies[tag].Now
		}
	}
	return out, nil
}

// SetSingboxSubscriptionEnabled switches one subscription. Only Enabled
// is sent: subscription.Service.Update reads a nil field as "not sent"
// and keeps the stored value, so a sparse patch is what leaves the
// filters, the mode and the URL alone.
//
// No invalidation event is published, because the REST handler publishes
// none: there is no subscription resource in internal/events, and an
// open web tab sees the change on its next fetch either way.
func (l *Local) SetSingboxSubscriptionEnabled(_ context.Context, id string, enabled bool) (mcpsrv.SingboxSubscription, []string, error) {
	if l.c.Subscriptions == nil {
		return mcpsrv.SingboxSubscription{}, nil, errUnavailable("sing-box subscriptions")
	}
	// Read first: the subscription may have been deleted since the agent
	// listed it, and the error should send it back to the list.
	current, err := l.c.Subscriptions.Get(id)
	if err != nil || current == nil {
		return mcpsrv.SingboxSubscription{}, nil, fmt.Errorf("sing-box subscription %q not found (use list_singbox_subscriptions)", id)
	}
	// Read outside the service's per-subscription lock: Update reads the
	// value again under it and does not return what it saw. If another
	// client switches the subscription between the two reads, the
	// warnings below describe a change that did not happen, or miss one
	// that did. The record returned is right either way.
	changed := current.Enabled != enabled
	label := sanitizeLabel(current.Label)

	updated, err := l.c.Subscriptions.Update(id, subscription.UpdatePatch{Enabled: &enabled})
	if err != nil {
		// The cause goes neither to the model nor into this line: it is
		// free text that can quote the subscription's address or its
		// content. The service journals it itself, under singbox/runtime
		// (subscription.Service.SetAppLogger); this line only marks the
		// attempt as MCP's.
		l.subLog.Warn("subscription-update", label, "Failed to switch subscription "+onOff(enabled)+" (MCP); the service journalled the cause under singbox/runtime")
		return mcpsrv.SingboxSubscription{}, nil, l.switchFailure(id, current.Enabled, enabled)
	}
	if updated == nil {
		return mcpsrv.SingboxSubscription{}, nil, fmt.Errorf("subscription update returned no record")
	}
	l.subLog.Info("subscription-update", label, "Subscription switched "+onOff(enabled)+" (MCP)")

	var warnings []string
	if changed {
		// resolveGroupTags skips a disabled subscription, so every enabled
		// aggregate group that lists this one just changed its members.
		for _, g := range l.c.Subscriptions.ListGroups() {
			if g.Enabled && slices.Contains(g.UseSubscriptionIDs, id) {
				warnings = append(warnings, fmt.Sprintf("aggregate group %q (%s) lists this subscription, so its set of servers may have changed — get_singbox_outbound shows it", sanitizeLabel(g.Label), g.Tag))
			}
		}
	}
	return singboxSubscription(updated), warnings, nil
}

// switchFailure says what state a failed switch left behind. It reads the
// subscription again rather than assume: the service restores the previous
// value when applying fails, but that restore can fail too
// (subscription.Service.Update, "rollback"), and the subscription can be
// deleted between our read and the write.
func (l *Local) switchFailure(id string, before, wanted bool) error {
	const where = "The cause is in the journal — get_logs, group singbox"
	after, err := l.c.Subscriptions.Get(id)
	switch {
	case err != nil || after == nil:
		return fmt.Errorf("sing-box subscription %q not found (use list_singbox_subscriptions)", id)
	case after.Enabled != before:
		return fmt.Errorf("the subscription is now STORED as %s, but applying that to sing-box failed and the previous value could not be restored. The stored setting and the running engine disagree, and the stored one takes effect when sing-box next reloads. Tell the user. %s", onOff(after.Enabled), where)
	}
	return fmt.Errorf("the subscription could not be switched %s and is unchanged: it is still %s. %s", onOff(wanted), onOff(before), where)
}
