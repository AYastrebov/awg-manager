package signature

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// MaxTagBytes is the per-tag ceiling for a single <r>/<rc>/<rd> token.
//
// Provenance, because the number is widely miscited as a current AmneziaWG
// rule: it was enforced only by amneziawg-go, in newRandomGeneratorBase
// (device/awg/tag_generator.go:73 as of v0.2.15) — "size must be less than
// 1000" — and was deleted on 2025-12-01 by 0361c54 ("fix: refactor processing
// of junk packets", PR #103). Neither amneziawg-tools nor the AmneziaWG Linux
// kernel module ever had it, and awg_proxy.ko allows up to 100000
// (kmod/awg-proxy/src/cps.c).
//
// So no current AmneziaWG implementation rejects an oversized token, and a
// config carrying one is not malformed by today's upstream. Keenetic's NDMS
// ASC nonetheless refuses such a value, which is consistent with its parser
// having been derived from pre-PR-103 amneziawg-go. We keep our own generated
// chains under the limit (splitPad) and split third-party ones at the NDMS
// boundary purely for that compatibility.
const MaxTagBytes = 1000

// randTagRe matches one random-padding token. The size is required: <r> with
// no digits is rejected by the AmneziaWG parser anyway, so leaving it alone is
// the correct behaviour. Whitespace is tolerated because third-party
// generators are under no obligation to match our formatting.
var randTagRe = regexp.MustCompile(`<\s*(rc|rd|r)\s*(\d+)\s*>`)

// SplitOversizedTags rewrites any <r>/<rc>/<rd> token larger than MaxTagBytes
// into a run of tokens of the same kind that sum to the original size, and
// returns everything else — <b>, <t>, <c>, unknown tokens, stray text —
// byte-identical.
//
// The rewrite is wire-equivalent: N random bytes emitted as one token or as
// several are the same N bytes on the wire, so a peer sees no difference and
// already-distributed configs stay valid.
//
// This exists because generators in the wild legitimately emit tokens over the
// limit — it is no longer an upstream rule (see MaxTagBytes). The
// docker-amneziawg container's default I1 is a QUIC Initial padded to the RFC
// 9000 §14.1 minimum of 1200 bytes, whose payload lands in a single <r 1178>;
// a QUIC Initial cannot be expressed within 1000-byte tokens at all without
// splitting. Every current datapath accepts it, so it works everywhere until
// the value reaches NDMS, which refuses the whole interface with
// `"WireguardN": invalid I1 value.` — naming the slot but not the token.
func SplitOversizedTags(spec string) string {
	if !strings.Contains(spec, "<") {
		return spec
	}
	return randTagRe.ReplaceAllStringFunc(spec, func(tok string) string {
		m := randTagRe.FindStringSubmatch(tok)
		if m == nil {
			return tok
		}
		n, err := strconv.Atoi(m[2])
		// A size we cannot parse (overflow) or one already within the limit is
		// left exactly as it came in.
		if err != nil || n <= MaxTagBytes {
			return tok
		}
		return splitPad(n, m[1])
	})
}

// HasOversizedTag reports whether spec contains a token SplitOversizedTags
// would rewrite. Callers use it to log the fixup rather than perform it
// silently — a config that only imports because we edited it should say so.
func HasOversizedTag(spec string) bool {
	for _, m := range randTagRe.FindAllStringSubmatch(spec, -1) {
		if n, err := strconv.Atoi(m[2]); err == nil && n > MaxTagBytes {
			return true
		}
	}
	return false
}

// DescribeOversizedTags renders the offending tokens for a log line, e.g.
// "<r 1178> → <r 1000><r 178>". Returns "" when nothing needs splitting.
func DescribeOversizedTags(spec string) string {
	var parts []string
	for _, m := range randTagRe.FindAllStringSubmatch(spec, -1) {
		n, err := strconv.Atoi(m[2])
		if err != nil || n <= MaxTagBytes {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s → %s", m[0], splitPad(n, m[1])))
	}
	return strings.Join(parts, ", ")
}
