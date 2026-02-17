package session

import "strings"

// GetProviderSessionID returns the persisted provider-native session ID.
func GetProviderSessionID(entry SessionEntry, provider string) string {
	key := normalizeProviderKey(provider)
	if key == "" || len(entry.ProviderSessionIDs) == 0 {
		return ""
	}
	return strings.TrimSpace(entry.ProviderSessionIDs[key])
}

// SetProviderSessionID stores a provider-native session ID in the session entry.
func SetProviderSessionID(entry *SessionEntry, provider, id string) {
	if entry == nil {
		return
	}
	key := normalizeProviderKey(provider)
	value := strings.TrimSpace(id)
	if key == "" || value == "" {
		return
	}
	if entry.ProviderSessionIDs == nil {
		entry.ProviderSessionIDs = map[string]string{}
	}
	entry.ProviderSessionIDs[key] = value
}

// ClearProviderSessionID removes the provider-native session ID from the session entry.
func ClearProviderSessionID(entry *SessionEntry, provider string) {
	if entry == nil || len(entry.ProviderSessionIDs) == 0 {
		return
	}
	key := normalizeProviderKey(provider)
	if key == "" {
		return
	}
	delete(entry.ProviderSessionIDs, key)
	if len(entry.ProviderSessionIDs) == 0 {
		entry.ProviderSessionIDs = nil
	}
}

func normalizeProviderKey(provider string) string {
	key := strings.ToLower(strings.TrimSpace(provider))
	switch key {
	case "claudecode", "codex":
		return key
	default:
		return key
	}
}
