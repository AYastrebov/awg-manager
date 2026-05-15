package router

import (
	"testing"
)

func TestListPresetsContainsYoutube(t *testing.T) {
	presets := ListPresets()
	var found bool
	for _, p := range presets {
		if p.ID == "youtube" {
			found = true
			if len(p.RuleSets) == 0 || len(p.Rules) == 0 {
				t.Error("youtube preset missing rulesets or rules")
			}
		}
	}
	if !found {
		t.Error("youtube preset not in list")
	}
}

func TestPresetYoutubeAppliesRuleSetAndRule(t *testing.T) {
	cfg := NewEmptyConfig()
	if err := ApplyPresetToConfig(cfg, "youtube", "Germany VLESS"); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Route.RuleSet) != 1 || cfg.Route.RuleSet[0].Tag != "geosite-youtube" {
		t.Errorf("rule_set: %+v", cfg.Route.RuleSet)
	}
	if len(cfg.Route.Rules) != 1 || cfg.Route.Rules[0].Outbound != "Germany VLESS" {
		t.Errorf("rules: %+v", cfg.Route.Rules)
	}
}

func TestPresetAdsAppliesReject(t *testing.T) {
	cfg := NewEmptyConfig()
	if err := ApplyPresetToConfig(cfg, "ads", ""); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Route.Rules) != 1 || cfg.Route.Rules[0].Action != "reject" {
		t.Errorf("rules: %+v", cfg.Route.Rules)
	}
}

func TestPresetTunnelRequiresOutbound(t *testing.T) {
	cfg := NewEmptyConfig()
	err := ApplyPresetToConfig(cfg, "youtube", "")
	if err == nil {
		t.Error("expected error when outbound empty for tunnel preset")
	}
}

func TestPresetReAddRuleSet(t *testing.T) {
	cfg := NewEmptyConfig()
	if err := ApplyPresetToConfig(cfg, "youtube", "Germany"); err != nil {
		t.Fatal(err)
	}
	if err := ApplyPresetToConfig(cfg, "youtube", "France"); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Route.RuleSet) != 1 {
		t.Errorf("rule_set should not duplicate: %+v", cfg.Route.RuleSet)
	}
	if len(cfg.Route.Rules) != 2 {
		t.Errorf("expected 2 rules, got %d", len(cfg.Route.Rules))
	}
}

func TestPresetUnknown(t *testing.T) {
	cfg := NewEmptyConfig()
	err := ApplyPresetToConfig(cfg, "nonexistent", "")
	if err == nil || !isSubstring(err.Error(), "not found") {
		t.Errorf("expected not found error, got %v", err)
	}
}

func isSubstring(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestApplyPresetToConfig_IdempotentOnRules(t *testing.T) {
	cfg := &RouterConfig{}
	if err := ApplyPresetToConfig(cfg, "youtube", "awg-vpn0"); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	rulesAfterFirst := len(cfg.Route.Rules)
	ruleSetsAfterFirst := len(cfg.Route.RuleSet)

	if err := ApplyPresetToConfig(cfg, "youtube", "awg-vpn0"); err != nil {
		t.Fatalf("second apply: %v", err)
	}

	if got := len(cfg.Route.Rules); got != rulesAfterFirst {
		t.Errorf("Rules duplicated on second apply: %d -> %d", rulesAfterFirst, got)
	}
	if got := len(cfg.Route.RuleSet); got != ruleSetsAfterFirst {
		t.Errorf("RuleSets duplicated on second apply: %d -> %d", ruleSetsAfterFirst, got)
	}
}

func TestApplyPresetToConfig_DifferentOutboundCreatesSeparateRule(t *testing.T) {
	cfg := &RouterConfig{}
	if err := ApplyPresetToConfig(cfg, "youtube", "awg-vpn0"); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	rulesAfterFirst := len(cfg.Route.Rules)

	if err := ApplyPresetToConfig(cfg, "youtube", "awg-vpn1"); err != nil {
		t.Fatalf("second apply: %v", err)
	}

	if got := len(cfg.Route.Rules); got != rulesAfterFirst+1 {
		t.Errorf("expected new rule for different outbound: %d -> %d", rulesAfterFirst, got)
	}
}
