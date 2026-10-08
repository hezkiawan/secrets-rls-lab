// Package repository is the ONLY place with SQL.
// Each function: get a connection, run SQL, turn rows into models.
package repository

import (
	"context"

	"secrets-rls-lab/api/database"
	"secrets-rls-lab/api/models"
)

// The users and companies tables have no RLS in this lab (they are a "directory"),
// so these queries run directly on the pool, without BeginWithTenant.
const selectUsers = `
SELECT u.id::text, u.company_id::text, co.name, u.role, u.name, u.email
FROM support.users u
JOIN support.companies co ON co.id = u.company_id`

// ListUsers returns all users (for the demo login buttons).
func ListUsers() ([]models.User, error) {
	pool := database.GetPool()

	rows, err := pool.Query(context.Background(), selectUsers+" ORDER BY co.name, u.role DESC, u.name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []models.User
	for rows.Next() {
		var user models.User
		err := rows.Scan(&user.ID, &user.CompanyID, &user.Company, &user.Role, &user.Name, &user.Email)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

// FindUserByEmail returns one user. If nobody has that email, err is pgx.ErrNoRows.
func FindUserByEmail(email string) (models.User, error) {
	pool := database.GetPool()

	var user models.User
	err := pool.QueryRow(context.Background(), selectUsers+" WHERE u.email = $1", email).
		Scan(&user.ID, &user.CompanyID, &user.Company, &user.Role, &user.Name, &user.Email)
	return user, err
}
