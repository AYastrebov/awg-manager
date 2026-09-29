package localdeps

import (
	"context"
	"strings"
	"time"

	mcpsrv "github.com/hoaxisr/awg-manager/internal/mcp"
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
