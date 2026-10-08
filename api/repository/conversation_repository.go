package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"secrets-rls-lab/api/database"
	"secrets-rls-lab/api/models"
)

// IMPORTANT: no query in this file has "WHERE company_id = ...".
// Postgres Row Level Security adds that filter, using the values BeginWithTenant sets.

const selectConversations = `
SELECT c.id::text, co.name, COALESCE(u.name, ''), c.customer_name, c.channel, c.subject, c.status
FROM support.conversations c
JOIN support.companies co ON co.id = c.company_id
LEFT JOIN support.users u ON u.id = c.assigned_to
ORDER BY co.name, c.created_at, c.customer_name`

// ListConversations returns the conversations this user is allowed to see.
func ListConversations(user models.User) ([]models.Conversation, error) {
	ctx := context.Background()

	// 1. Start a transaction and tell Postgres who is asking.
	tx, err := database.BeginWithTenant(database.GetPool(), user)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) // does nothing if we already committed

	// 2. Run the query. RLS filters the rows.
	conversations, err := readConversations(tx)
	if err != nil {
		return nil, err
	}

	// 3. Commit. The "who is asking" values disappear.
	err = tx.Commit(ctx)
	return conversations, err
}

// UpdateStatus changes one conversation's status. Returns how many rows changed.
// 0 means "doesn't exist" OR "not allowed": RLS hides rows you may not touch.
func UpdateStatus(user models.User, conversationID string, status string) (int64, error) {
	ctx := context.Background()

	tx, err := database.BeginWithTenant(database.GetPool(), user)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	result, err := tx.Exec(ctx,
		"UPDATE support.conversations SET status = $2 WHERE id = $1::uuid",
		conversationID, status)
	if err != nil {
		return 0, err
	}

	err = tx.Commit(ctx)
	return result.RowsAffected(), err
}

// CreateConversation inserts a new, unassigned conversation into companyID.
// If companyID is not the user's own company, RLS refuses it (error 42501).
func CreateConversation(user models.User, companyID string, customerName string, channel string, subject string) error {
	ctx := context.Background()

	tx, err := database.BeginWithTenant(database.GetPool(), user)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx,
		`INSERT INTO support.conversations (company_id, customer_name, channel, subject)
		 VALUES ($1::uuid, $2, $3, $4)`,
		companyID, customerName, channel, subject)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// ---- RLS demos (lab only) -------------------------------------------------------

// ListConversationsWithoutContext runs the same query but "forgets" to say who is
// asking. Expected result: 0 rows (fail-safe).
func ListConversationsWithoutContext() ([]models.Conversation, error) {
	ctx := context.Background()

	tx, err := database.GetPool().Begin(ctx) // NOTE: no BeginWithTenant
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	conversations, err := readConversations(tx)
	if err != nil {
		return nil, err
	}
	err = tx.Commit(ctx)
	return conversations, err
}

// ListConversationsAsOwner runs the same query connected as the table OWNER.
// Expected result: ALL companies (the owner skips RLS), unless FORCE is turned on.
func ListConversationsAsOwner(user models.User) ([]models.Conversation, error) {
	ctx := context.Background()

	tx, err := database.BeginWithTenant(database.OwnerPool, user) // NOTE: owner's pool
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	conversations, err := readConversations(tx)
	if err != nil {
		return nil, err
	}
	err = tx.Commit(ctx)
	return conversations, err
}

// readConversations runs the SELECT inside a transaction and turns rows into models.
func readConversations(tx pgx.Tx) ([]models.Conversation, error) {
	rows, err := tx.Query(context.Background(), selectConversations)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	conversations := []models.Conversation{} // empty list (not nil) so JSON shows []
	for rows.Next() {
		var c models.Conversation
		err := rows.Scan(&c.ID, &c.Company, &c.AssignedTo, &c.CustomerName, &c.Channel, &c.Subject, &c.Status)
		if err != nil {
			return nil, err
		}
		conversations = append(conversations, c)
	}
	return conversations, rows.Err()
}
