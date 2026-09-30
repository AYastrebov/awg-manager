package logging

import (
	"net/url"
	"regexp"
)

var (
	// A scheme, "://", and a tail that starts like a host or userinfo:
	// the daemon's own hints list bare schemes ("vless://, trojan://"),
	// and those are not addresses. The tail stops at whitespace, quotes
	// and brackets, which is how net/http and url.Parse quote a URL.
	urlInTextRe = regexp.MustCompile("(?i)\\b([a-z][a-z0-9+.-]{0,15})://([A-Za-z0-9\\[][^\\s\"'<>()`]*)")
	// A host worth keeping has a dot (or is a bracketed IPv6 literal).
	// The legacy ss:// link is one base64 blob that url.Parse reads as
	// the host; base64 has no dot, so it never passes.
	keptHostRe = regexp.MustCompile(`^(?:(?:[A-Za-z0-9-]+\.)+[A-Za-z0-9-]+|\[[0-9A-Fa-f:.]+\])(?::[0-9]{1,5})?$`)
)

// RedactURLs reduces every URL in s to its scheme and host. A
// subscription's address carries its token in the path, the query or the
// userinfo, and a share link carries the server's uuid in the userinfo;
// net/http and url.Parse quote such an address whole in their errors —
// the address of a redirect hop too, which no exact-match replacement of
// the configured URL can catch. The host is what a reader needs to tell
// which download failed, and SanitizeLogText masks it further on display.
//
// Apply it where a message is written, not where it is shown: the stored
// text is what REST, the web interface, the journal and MCP all read.
func RedactURLs(s string) string {
	return urlInTextRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := urlInTextRe.FindStringSubmatch(m)
		scheme := sub[1]
		u, err := url.Parse(m)
		if err != nil || !keptHostRe.MatchString(u.Host) {
			return scheme + "://<redacted>"
		}
		if u.User == nil && (u.Path == "" || u.Path == "/") && u.RawQuery == "" && u.Fragment == "" && u.Opaque == "" {
			return scheme + "://" + u.Host + u.Path
		}
		return scheme + "://" + u.Host + "/…"
	})
}
