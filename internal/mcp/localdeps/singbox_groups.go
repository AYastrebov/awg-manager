package localdeps

import (
	"context"
	"time"

	mcpsrv "github.com/hoaxisr/awg-manager/internal/mcp"
	"github.com/hoaxisr/awg-manager/internal/singbox/subscription"
)

// singboxSubscription maps a subscription field by field. Nothing that
// can carry the provider's token is read here: not the URL beyond its
// host, not the headers, not the pasted body, not the file path.
func singboxSubscription(s *subscription.Subscription) mcpsrv.SingboxSubscription {
	out := mcpsrv.SingboxSubscription{
		ID:            s.ID,
		Label:         sanitizeLabel(s.Label),
		Source:        "url",
		Host:          hostOf(s.URL),
		Enabled:       s.Enabled,
		Mode:          string(s.EffectiveMode()),
		GroupTag:      s.SelectorTag,
		MemberCount:   len(s.MemberTags),
		ExcludedCount: len(s.ExcludedTags),
		OrphanCount:   len(s.OrphanTags),
		RefreshHours:  s.RefreshHours,
		// The store masks the exact subscription URL when it records the
		// error; maskURLs covers the same secret spelled differently.
		LastError: maskURLs(subscription.MaskURL(s.LastError, s.URL)),
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
