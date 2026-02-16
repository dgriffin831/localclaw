package tools

import "testing"

func TestNewPolicyRejectsBlankAndDuplicateEntries(t *testing.T) {
	if _, err := NewPolicy([]string{"ok", "  "}, nil); err == nil {
		t.Fatalf("expected blank allow entry error")
	}
	if _, err := NewPolicy(nil, []string{"deny", "DENY"}); err == nil {
		t.Fatalf("expected duplicate deny entry error")
	}
}

func TestPolicyDenyOverridesAllow(t *testing.T) {
	policy, err := NewPolicy([]string{"memory_search"}, []string{"memory_search"})
	if err != nil {
		t.Fatalf("NewPolicy error: %v", err)
	}
	allowed, reason := policy.Allowed("memory_search")
	if allowed {
		t.Fatalf("expected deny to override allow")
	}
	if reason == "" {
		t.Fatalf("expected deny reason")
	}
}
