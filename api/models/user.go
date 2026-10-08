// Package models holds the data shapes the API works with. No logic here.
package models

// User is a person using the support desk (Ana, Budi, Sari, ...).
// The json tags are the field names the demo page expects.
type User struct {
	ID        string `json:"user_id"`
	CompanyID string `json:"company_id"`
	Company   string `json:"company"` // company name, e.g. "Acme Retail"
	Role      string `json:"role"`    // "agent" or "supervisor"
	Name      string `json:"name"`
	Email     string `json:"email"`
}
