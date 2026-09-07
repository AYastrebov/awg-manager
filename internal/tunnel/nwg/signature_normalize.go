package nwg

import (
	"strings"

	"github.com/hoaxisr/awg-manager/internal/signature"
	"github.com/hoaxisr/awg-manager/internal/storage"
	"github.com/hoaxisr/awg-manager/internal/tunnel/config"
)

// NDMS parses I1-I5 with the native AmneziaWG parser, which enforces the
// per-tag ceiling on <r>/<rc>/<rd> that our own datapaths do not: awg_proxy.ko
// accepts any size up to 100000 (kmod/awg-proxy/src/cps.c), and so do the
// userspace and kernel AmneziaWG implementations. A config generated elsewhere
// therefore works everywhere until it reaches the router, which refuses the
// whole interface with `"WireguardN": invalid I1 value.` and creates nothing.
//
// The known source is the docker-amneziawg container, whose default I1 is a
// QUIC Initial padded to the RFC 9000 §14.1 minimum of 1200 bytes — its
// payload is a single <r 1178>. That default ships in every config the
// container hands out, so the configs already in users' hands cannot be
// regenerated. Splitting the token is wire-equivalent (same total random
// bytes), so the peer sees no change and distributed configs stay valid.
//
// Only what goes to NDMS is rewritten. The stored tunnel keeps the signature
// exactly as imported, so a .conf downloaded from the UI is still the file the
// user gave us.

// splitSignatureTags returns iface with every oversized <r>/<rc>/<rd> token in
// I1-I5 split, plus a description of what changed for the log ("" if nothing).
// The input is not modified.
func splitSignatureTags(iface *storage.AWGInterface) (storage.AWGInterface, string) {
	out := *iface
	slots := []struct {
		name string
		val  *string
	}{
		{"I1", &out.I1}, {"I2", &out.I2}, {"I3", &out.I3},
		{"I4", &out.I4}, {"I5", &out.I5},
	}

	var notes []string
	for _, s := range slots {
		if !signature.HasOversizedTag(*s.val) {
			continue
		}
		notes = append(notes, s.name+": "+signature.DescribeOversizedTags(*s.val))
		*s.val = signature.SplitOversizedTags(*s.val)
	}
	return out, strings.Join(notes, "; ")
}

// ndmsImportConf renders the .conf uploaded to NDMS with oversized signature
// tokens split, and a description of what was split for the log ("" if
// nothing). Byte-identical to config.GenerateForExport for any config the
// router would have accepted anyway.
func ndmsImportConf(stored *storage.AWGTunnel) (string, string) {
	safe := *stored
	iface, note := splitSignatureTags(&stored.Interface)
	safe.Interface = iface
	return config.GenerateForExport(&safe), note
}
