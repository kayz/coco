package promptbuild

// HistorySpec defines how to fetch chat history from SQLite.
type HistorySpec struct {
	ConversationID int64  `json:"conversation_id,omitempty"`
	Platform       string `json:"platform,omitempty"`
	ChannelID      string `json:"channel_id,omitempty"`
	UserID         string `json:"user_id,omitempty"`
	Limit          int    `json:"limit,omitempty"`
}

// BuildRequest defines inputs for prompt assembly.
type BuildRequest struct {
	System       []string    `json:"system,omitempty"`
	Task         []string    `json:"task,omitempty"`
	Format       []string    `json:"format,omitempty"`
	Style        []string    `json:"style,omitempty"`
	Requirements string      `json:"requirements,omitempty"`
	References   []string    `json:"references,omitempty"`
	History      HistorySpec `json:"history,omitempty"`
	UserInput    string      `json:"user_input,omitempty"`

	// Agent selects a named prompt assembly spec.
	Agent string `json:"agent,omitempty"`
	// SpecPath explicitly sets the prompt assembly spec file path.
	SpecPath string `json:"spec_path,omitempty"`
	// Inputs provides additional values consumed by spec sections.
	Inputs map[string]string `json:"inputs,omitempty"`

	// MaxHistory is a fallback limit when History.Limit is not set.
	MaxHistory int `json:"max_history,omitempty"`

	// IncludeSectionHeaders controls whether section headings are included.
	IncludeSectionHeaders *bool `json:"include_section_headers,omitempty"`
}
