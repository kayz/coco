package search

import "time"

type SearchResult struct {
	Title       string    `json:"title"`
	URL         string    `json:"url"`
	Snippet     string    `json:"snippet"`
	Content     string    `json:"content,omitempty"`
	Source      string    `json:"source"`
	PublishedAt time.Time `json:"published_at"`
	RetrievedAt time.Time `json:"retrieved_at"`
	Score       float64   `json:"score,omitempty"`
}

type SearchResponse struct {
	Query      string          `json:"query"`
	Results    []SearchResult  `json:"results"`
	Engine     string          `json:"engine"`
	Duration   time.Duration   `json:"duration"`
	TotalCount int             `json:"total_count,omitempty"`
}

type CombinedSearchResponse struct {
	Query     string                  `json:"query"`
	Responses map[string]SearchResponse `json:"responses"`
	Combined  []SearchResult          `json:"combined"`
	Analysis  string                  `json:"analysis,omitempty"`
}
