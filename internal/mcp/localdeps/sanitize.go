package localdeps

import (
	"net/url"
	"strings"
	"unicode"

	mcpsrv "github.com/hoaxisr/awg-manager/internal/mcp"
)

// sanitizeLabel makes third-party text safe to hand to a model. A
// provider chooses its server names, and a name with a newline in it is a
// second line in the model's context. Control characters and line
// separators become a space, format characters (bidi overrides,
// zero-width) are dropped, runs of space collapse, and the result is
// capped on a rune boundary.
func sanitizeLabel(s string) string {
	spaced := strings.Map(func(r rune) rune {
		switch {
		case unicode.IsControl(r), r == ' ', r == ' ':
			return ' '
		case unicode.Is(unicode.Cf, r):
			return -1
		}
		return r
	}, s)
	clean := strings.Join(strings.Fields(spaced), " ")
	if runes := []rune(clean); len(runes) > mcpsrv.MaxSingboxLabelRunes {
		clean = strings.TrimSpace(string(runes[:mcpsrv.MaxSingboxLabelRunes]))
	}
	return clean
}

// hostOf returns the host of a subscription URL and nothing else: the
// token usually sits in the path, which is why redactURL (host and path)
// is not used for subscriptions.
func hostOf(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return u.Host
}
