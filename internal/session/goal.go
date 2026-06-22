package session

// Goal represents a session goal item.
type Goal struct {
	Description string `json:"description"`
	Status      string `json:"status"`
	Target      string `json:"target,omitempty"`
}
