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

// MaxSplittableTagBytes bounds what we are willing to rewrite. A size beyond
// it is not a config we can rescue: awg_proxy.ko's own parser rejects anything
// over 100000 (parse_int in kmod/awg-proxy/src/cps.c), so such a token is
// nonsense wherever it ends up.
//
// The bound is load-bearing, not cosmetic. Nothing validates I1-I5 on the way
// in — config.Parse stores the string verbatim and ValidateAWG3 checks only
// HeaderProtectionKey and S1-S4 — so an imported .conf can carry
// "<r 999999999999999999>". Expanding that would spin ~10^15 iterations
// appending to a Builder and take the process out on memory. Oversized beyond
// rescue is left exactly as it came in, to be rejected downstream as it
// should be.
const MaxSplittableTagBytes = 100000

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
		if err != nil || n > MaxSplittableTagBytes {
			return tok
		}
		if n > MaxTagBytes {
			return splitPad(n, m[1])
		}
		// Within the limit, but still re-emitted in canonical "<kind N>" form.
		// The regex tolerates "<r500>" and "<r  500 >", which upstream's own
		// parser does not accept — its parseTag regex,
		// `([a-zA-Z]+)(?:\s+([^>]+))?>`, requires the whitespace. Rewriting
		// only the oversized tokens would "fix" a config and leave a
		// differently-malformed one in it, so every token we match is
		// normalized.
		return fmt.Sprintf("<%s %d>", m[1], n)
	})
}

// HasOversizedTag reports whether spec contains a token SplitOversizedTags
// would rewrite. Callers use it to log the fixup rather than perform it
// silently — a config that only imports because we edited it should say so.
func HasOversizedTag(spec string) bool {
	for _, m := range randTagRe.FindAllStringSubmatch(spec, -1) {
		n, err := strconv.Atoi(m[2])
		if err == nil && n > MaxTagBytes && n <= MaxSplittableTagBytes {
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
		if err != nil || n <= MaxTagBytes || n > MaxSplittableTagBytes {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s → %s", m[0], splitPad(n, m[1])))
	}
	return strings.Join(parts, ", ")
}
