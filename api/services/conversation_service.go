package services

import (
	"secrets-rls-lab/api/models"
	"secrets-rls-lab/api/repository"
)

// ListConversations: no extra rules. RLS decides which rows come back.
func ListConversations(user models.User) ([]models.Conversation, error) {
	return repository.ListConversations(user)
}

// ChangeStatus updates the status. Returns false if nothing was changed, which means
// "not found or not allowed" (RLS hides rows the user may not touch).
func ChangeStatus(user models.User, conversationID string, status string) (bool, error) {
	rowsChanged, err := repository.UpdateStatus(user, conversationID, status)
	if err != nil {
		return false, err
	}
	return rowsChanged > 0, nil
}

// NewConversation is what the client sends to create a conversation.
type NewConversation struct {
	CustomerName string `json:"customer_name"`
	Channel      string `json:"channel"`
	Subject      string `json:"subject"`

	// DEMO ONLY: lets the demo page TRY to write into another company.
	// RLS must refuse it. A real API would not have this field.
	SpoofCompanyID string `json:"spoof_company_id"`
}

// CreateConversation applies two small rules, then saves. Returns the company used.
func CreateConversation(user models.User, input NewConversation) (string, error) {
	// Rule 1: default channel.
	channel := input.Channel
	if channel == "" {
		channel = "webchat"
	}

	// Rule 2: new conversations go into the user's own company.
	companyID := user.CompanyID
	if input.SpoofCompanyID != "" {
		companyID = input.SpoofCompanyID // demo: try another company → RLS blocks it
	}

	err := repository.CreateConversation(user, companyID, input.CustomerName, channel, input.Subject)
	return companyID, err
}

// ---- RLS demos (lab only) -------------------------------------------------------

func ListConversationsWithoutContext() ([]models.Conversation, error) {
	return repository.ListConversationsWithoutContext()
}

func ListConversationsAsOwner(user models.User) ([]models.Conversation, error) {
	return repository.ListConversationsAsOwner(user)
}
