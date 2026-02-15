package config

import "testing"

func TestDefaultConfigIsValid(t *testing.T) {
	cfg := Default()
	if err := cfg.Validate(); err != nil {
		fatalf(t, "expected default config to validate, got error: %v", err)
	}
}

func TestValidateRejectsUnsupportedChannel(t *testing.T) {
	cfg := Default()
	cfg.Channels.Enabled = []string{"slack", "teams"}
	if err := cfg.Validate(); err == nil {
		fatalf(t, "expected unsupported channel error")
	}
}

func TestValidateRejectsNetworkServerFlags(t *testing.T) {
	cfg := Default()
	cfg.Security.EnableHTTPServer = true
	if err := cfg.Validate(); err == nil {
		fatalf(t, "expected local-only policy rejection")
	}
}

func TestValidateRejectsUnsupportedClaudeAuthMode(t *testing.T) {
	cfg := Default()
	cfg.LLM.ClaudeCode.AuthMode = "oidc"
	if err := cfg.Validate(); err == nil {
		fatalf(t, "expected unsupported auth mode error")
	}
}

func TestValidateRequiresGovCloudRegion(t *testing.T) {
	cfg := Default()
	cfg.LLM.ClaudeCode.UseGovCloud = true
	cfg.LLM.ClaudeCode.BedrockRegion = ""
	if err := cfg.Validate(); err == nil {
		fatalf(t, "expected govcloud region error")
	}
}

func fatalf(t *testing.T, format string, args ...interface{}) {
	t.Helper()
	t.Fatalf(format, args...)
}
