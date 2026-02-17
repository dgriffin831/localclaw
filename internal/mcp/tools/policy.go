package tools

import (
	"fmt"
	"strings"
)

type Policy struct {
	allow map[string]struct{}
	deny  map[string]struct{}
}

func NewPolicy(allow []string, deny []string) (Policy, error) {
	normalizedAllow, err := normalizePolicyEntries(allow, "allow")
	if err != nil {
		return Policy{}, err
	}
	normalizedDeny, err := normalizePolicyEntries(deny, "deny")
	if err != nil {
		return Policy{}, err
	}
	return Policy{allow: normalizedAllow, deny: normalizedDeny}, nil
}

func (p Policy) Allowed(toolName string) (bool, string) {
	name := strings.ToLower(strings.TrimSpace(toolName))
	if name == "" {
		return false, "tool name is required"
	}
	if _, denied := p.deny[name]; denied {
		return false, fmt.Sprintf("tool %q denied by policy", name)
	}
	if len(p.allow) > 0 {
		if _, ok := p.allow[name]; !ok {
			return false, fmt.Sprintf("tool %q is not allowlisted", name)
		}
	}
	return true, ""
}

func normalizePolicyEntries(values []string, field string) (map[string]struct{}, error) {
	out := make(map[string]struct{}, len(values))
	for i, raw := range values {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" {
			return nil, fmt.Errorf("%s[%d] cannot be blank", field, i)
		}
		if _, exists := out[name]; exists {
			return nil, fmt.Errorf("duplicate %s entry %q", field, name)
		}
		out[name] = struct{}{}
	}
	return out, nil
}
