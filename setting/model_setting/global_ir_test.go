package model_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/config"
)

func TestIRConversionEnabledDefaultAndDynamicToggle(t *testing.T) {
	t.Cleanup(func() {
		GetGlobalSettings().IRConversionEnabled = true
	})

	if !IsIRConversionEnabled() {
		t.Fatalf("expected IsIRConversionEnabled() to default to true, got false")
	}

	exported := config.GlobalConfig.ExportAllConfigs()
	if got, ok := exported["global.ir_conversion_enabled"]; !ok || got != "true" {
		t.Fatalf("expected exported config global.ir_conversion_enabled to be %q (present=true), got %q (present=%v)", "true", got, ok)
	}

	if err := config.UpdateConfigFromMap(GetGlobalSettings(), map[string]string{
		"ir_conversion_enabled": "false",
	}); err != nil {
		t.Fatalf("failed to disable ir_conversion_enabled via UpdateConfigFromMap: %v", err)
	}
	if IsIRConversionEnabled() {
		t.Fatalf("expected IsIRConversionEnabled() to be false after toggling off")
	}
	if got := config.GlobalConfig.ExportAllConfigs()["global.ir_conversion_enabled"]; got != "false" {
		t.Fatalf("expected exported global.ir_conversion_enabled to be %q after toggling off, got %q", "false", got)
	}

	if err := config.UpdateConfigFromMap(GetGlobalSettings(), map[string]string{
		"ir_conversion_enabled": "true",
	}); err != nil {
		t.Fatalf("failed to re-enable ir_conversion_enabled via UpdateConfigFromMap: %v", err)
	}
	if !IsIRConversionEnabled() {
		t.Fatalf("expected IsIRConversionEnabled() to be true after toggling back on")
	}
	if got := config.GlobalConfig.ExportAllConfigs()["global.ir_conversion_enabled"]; got != "true" {
		t.Fatalf("expected exported global.ir_conversion_enabled to be %q after toggling back on, got %q", "true", got)
	}
}
