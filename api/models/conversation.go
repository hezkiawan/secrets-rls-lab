package models

// Conversation is one customer conversation, as shown on the demo page.
type Conversation struct {
	ID           string `json:"id"`
	Company      string `json:"company"`
	AssignedTo   string `json:"assigned_to"` // agent's name, "" if unassigned
	CustomerName string `json:"customer_name"`
	Channel      string `json:"channel"`
	Subject      string `json:"subject"`
	Status       string `json:"status"`
}
