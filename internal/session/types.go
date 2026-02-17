package session

// Origin identifies where a session was initiated.
type Origin string

const (
	OriginUnknown  Origin = "unknown"
	OriginCLI      Origin = "cli"
	OriginSlack    Origin = "slack"
	OriginSignal   Origin = "signal"
	OriginSubagent Origin = "subagent"
)

// DeliveryMetadata captures channel-specific delivery identifiers.
type DeliveryMetadata struct {
	Channel   string `json:"channel,omitempty"`
	ThreadID  string `json:"threadId,omitempty"`
	MessageID string `json:"messageId,omitempty"`
	UserID    string `json:"userId,omitempty"`
}

// SessionEntry stores durable metadata for one session.
type SessionEntry struct {
	ID                         string            `json:"id"`
	Key                        string            `json:"key,omitempty"`
	AgentID                    string            `json:"agentId"`
	Origin                     Origin            `json:"origin,omitempty"`
	Delivery                   DeliveryMetadata  `json:"delivery,omitempty"`
	TranscriptPath             string            `json:"transcriptPath,omitempty"`
	TotalTokens                int               `json:"totalTokens,omitempty"`
	CompactionCount            int               `json:"compactionCount,omitempty"`
	BootstrapInjected          bool              `json:"bootstrapInjected,omitempty"`
	BootstrapCompactionCount   int               `json:"bootstrapCompactionCount,omitempty"`
	MemoryFlushAt              string            `json:"memoryFlushAt,omitempty"`
	MemoryFlushCompactionCount int               `json:"memoryFlushCompactionCount,omitempty"`
	ProviderSessionIDs         map[string]string `json:"providerSessionIds,omitempty"`
	CreatedAt                  string            `json:"createdAt"`
	UpdatedAt                  string            `json:"updatedAt"`
}
