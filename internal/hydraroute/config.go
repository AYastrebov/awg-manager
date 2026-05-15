package hydraroute

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// canonicalKey maps the lowercase form of each managed key to the
// casing hr-neo itself writes in /opt/etc/HydraRoute/hrneo.conf.
// Used only when appending a key that did not exist in the original
// file — for in-place rewrites we preserve whatever case the existing
// line used. Without this mapping our writer would invent a different
// case for the few keys where the daemon's convention isn't
// PascalCase (autoStart, clearIPSet, log, logfile), and the next
// daemon write would create a case-mismatched duplicate that breaks
// both reads and writes.
var canonicalKey = map[string]string{
	"autostart":          "autoStart",
	"clearipset":         "clearIPSet",
	"cidr":               "CIDR",
	"ipsetenabletimeout": "IpsetEnableTimeout",
	"ipsettimeout":       "IpsetTimeout",
	"ipsetmaxelem":       "IpsetMaxElem",
	"directrouteenabled": "DirectRouteEnabled",
	"globalrouting":      "GlobalRouting",
	"conntrackflush":     "ConntrackFlush",
	"log":                "log",
	"logfile":            "logfile",
	"geoipfile":          "GeoIPFile",
	"geositefile":        "GeoSiteFile",
	"policyorder":        "PolicyOrder",
}

// ReadConfig parses hrneo.conf and returns the managed Config fields.
// Unknown keys and comments are ignored; defaults are applied where
// needed. Key matching is case-insensitive — hr-neo's own writes mix
// PascalCase, camelCase, mixedCase, and lowercase for different
// fields, so we accept any casing and let WriteConfig dedupe.
func ReadConfig() (*Config, error) {
	f, err := os.Open(hrConfPath)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultConfig(), nil
		}
		return nil, fmt.Errorf("hydraroute: open hrneo.conf: %w", err)
	}
	defer f.Close()

	cfg := defaultConfig()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		// Strip inline comments (# not at start)
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = line[:idx]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)

		switch strings.ToLower(key) {
		case "autostart":
			cfg.AutoStart = parseBool(val)
		case "clearipset":
			cfg.ClearIPSet = parseBool(val)
		case "cidr":
			cfg.CIDR = parseBool(val)
		case "ipsetenabletimeout":
			cfg.IpsetEnableTimeout = parseBool(val)
		case "ipsettimeout":
			cfg.IpsetTimeout, _ = strconv.Atoi(val)
		case "ipsetmaxelem":
			cfg.IpsetMaxElem, _ = strconv.Atoi(val)
		case "directrouteenabled":
			cfg.DirectRouteEnabled = parseBool(val)
		case "globalrouting":
			cfg.GlobalRouting = parseBool(val)
		case "conntrackflush":
			cfg.ConntrackFlush = parseBool(val)
		case "log":
			cfg.Log = val
		case "logfile":
			cfg.LogFile = val
		case "geoipfile":
			if val != "" {
				cfg.GeoIPFiles = append(cfg.GeoIPFiles, val)
			}
		case "geositefile":
			if val != "" {
				cfg.GeoSiteFiles = append(cfg.GeoSiteFiles, val)
			}
		case "policyorder":
			cfg.PolicyOrder = nil
			for _, s := range strings.Split(val, ",") {
				s = strings.TrimSpace(s)
				if s != "" {
					cfg.PolicyOrder = append(cfg.PolicyOrder, s)
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("hydraroute: scan hrneo.conf: %w", err)
	}
	return cfg, nil
}

// WriteConfig updates hrneo.conf with the managed Config fields,
// preserving unknown keys, comments, and the EXACT casing each
// existing managed key already uses on disk. Subsequent occurrences
// of the same case-insensitive key are dropped, which heals files
// previously corrupted by the older case-sensitive writer (it left
// behind a daemon-cased line AND a PascalCase appended duplicate).
// Multi-value fields (GeoIPFile, GeoSiteFile) are written in full on
// the first occurrence; subsequent occurrences are dropped.
func WriteConfig(cfg *Config) error {
	if err := os.MkdirAll(hrDir, 0o755); err != nil {
		return fmt.Errorf("hydraroute: create hrneo dir: %w", err)
	}

	existing, err := os.ReadFile(hrConfPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("hydraroute: read hrneo.conf: %w", err)
	}

	// keyState is keyed by lowercase key, so any casing in the file is
	// recognised and the first occurrence (whatever its case) is the
	// one that survives.
	type keyState struct{ written bool }
	known := make(map[string]*keyState, len(canonicalKey))
	for k := range canonicalKey {
		known[k] = &keyState{}
	}

	var out strings.Builder

	if len(existing) > 0 {
		scanner := bufio.NewScanner(strings.NewReader(string(existing)))
		for scanner.Scan() {
			rawLine := scanner.Text()

			// Detect the key — strip comment for matching, but the
			// in-place replacement preserves the original raw form
			// when we write back.
			stripped := rawLine
			if idx := strings.Index(stripped, "#"); idx >= 0 {
				stripped = stripped[:idx]
			}
			stripped = strings.TrimSpace(stripped)

			origKey := ""
			if k, _, ok := strings.Cut(stripped, "="); ok {
				origKey = strings.TrimSpace(k)
			}

			lowerKey := strings.ToLower(origKey)
			state, isKnown := known[lowerKey]
			if !isKnown {
				// Preserve unknown lines as-is.
				out.WriteString(rawLine)
				out.WriteByte('\n')
				continue
			}

			if state.written {
				// Drop case-insensitive duplicates (heals files
				// where the old case-sensitive writer left both a
				// daemon-cased and a PascalCase line for the same key).
				continue
			}
			state.written = true

			// Replace with new value(s), preserving the original
			// key casing observed in this line.
			switch lowerKey {
			case "geoipfile":
				for _, v := range cfg.GeoIPFiles {
					fmt.Fprintf(&out, "%s=%s\n", origKey, v)
				}
				if len(cfg.GeoIPFiles) == 0 {
					fmt.Fprintf(&out, "%s=\n", origKey)
				}
			case "geositefile":
				for _, v := range cfg.GeoSiteFiles {
					fmt.Fprintf(&out, "%s=%s\n", origKey, v)
				}
				if len(cfg.GeoSiteFiles) == 0 {
					fmt.Fprintf(&out, "%s=\n", origKey)
				}
			default:
				fmt.Fprintf(&out, "%s=%s\n", origKey, configValue(lowerKey, cfg))
			}
		}
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("hydraroute: scan hrneo.conf: %w", err)
		}
	}

	// Append any managed keys absent from the original file using
	// canonicalKey to pick the casing hr-neo itself would have used.
	appendIfMissing := func(lowerKey string, value string) {
		state := known[lowerKey]
		if state.written {
			return
		}
		fmt.Fprintf(&out, "%s=%s\n", canonicalKey[lowerKey], value)
		state.written = true
	}

	appendIfMissing("autostart", formatBool(cfg.AutoStart))
	appendIfMissing("clearipset", formatBool(cfg.ClearIPSet))
	appendIfMissing("cidr", formatBool(cfg.CIDR))
	appendIfMissing("ipsetenabletimeout", formatBool(cfg.IpsetEnableTimeout))
	appendIfMissing("ipsettimeout", strconv.Itoa(cfg.IpsetTimeout))
	appendIfMissing("ipsetmaxelem", strconv.Itoa(cfg.IpsetMaxElem))
	appendIfMissing("directrouteenabled", formatBool(cfg.DirectRouteEnabled))
	appendIfMissing("globalrouting", formatBool(cfg.GlobalRouting))
	appendIfMissing("conntrackflush", formatBool(cfg.ConntrackFlush))
	appendIfMissing("log", cfg.Log)
	appendIfMissing("logfile", cfg.LogFile)
	appendIfMissing("policyorder", strings.Join(cfg.PolicyOrder, ","))
	if state := known["geoipfile"]; !state.written {
		for _, v := range cfg.GeoIPFiles {
			fmt.Fprintf(&out, "%s=%s\n", canonicalKey["geoipfile"], v)
		}
		if len(cfg.GeoIPFiles) == 0 {
			fmt.Fprintf(&out, "%s=\n", canonicalKey["geoipfile"])
		}
		state.written = true
	}
	if state := known["geositefile"]; !state.written {
		for _, v := range cfg.GeoSiteFiles {
			fmt.Fprintf(&out, "%s=%s\n", canonicalKey["geositefile"], v)
		}
		if len(cfg.GeoSiteFiles) == 0 {
			fmt.Fprintf(&out, "%s=\n", canonicalKey["geositefile"])
		}
		state.written = true
	}

	return atomicWrite(hrConfPath, out.String())
}

// configValue returns the string representation for a scalar managed key.
// lowerKey must already be lowercased.
func configValue(lowerKey string, cfg *Config) string {
	switch lowerKey {
	case "autostart":
		return formatBool(cfg.AutoStart)
	case "clearipset":
		return formatBool(cfg.ClearIPSet)
	case "cidr":
		return formatBool(cfg.CIDR)
	case "ipsetenabletimeout":
		return formatBool(cfg.IpsetEnableTimeout)
	case "ipsettimeout":
		return strconv.Itoa(cfg.IpsetTimeout)
	case "ipsetmaxelem":
		return strconv.Itoa(cfg.IpsetMaxElem)
	case "directrouteenabled":
		return formatBool(cfg.DirectRouteEnabled)
	case "globalrouting":
		return formatBool(cfg.GlobalRouting)
	case "conntrackflush":
		return formatBool(cfg.ConntrackFlush)
	case "log":
		return cfg.Log
	case "logfile":
		return cfg.LogFile
	case "policyorder":
		return strings.Join(cfg.PolicyOrder, ",")
	}
	return ""
}

// defaultConfig returns a Config with sensible defaults.
func defaultConfig() *Config {
	return &Config{
		DirectRouteEnabled: true,
		ConntrackFlush:     true,
	}
}

// parseBool returns true for "true", "1", or "yes" (case-insensitive).
func parseBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes":
		return true
	}
	return false
}

// formatBool returns "true" or "false".
func formatBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
