// Package services sits between handlers (HTTP) and repository (SQL).
// It is where business rules go.
//
// In this project it is deliberately THIN: most "who may do what" rules are enforced by
// Row Level Security in Postgres, so many functions just pass the call through.
// In a bigger app, rules like "only supervisors may reopen a closed conversation"
// would live here.
package services

import (
	"secrets-rls-lab/api/auth"
	"secrets-rls-lab/api/models"
	"secrets-rls-lab/api/repository"
)

// ListDemoUsers returns everyone who can log in on the demo page.
func ListDemoUsers() ([]models.User, error) {
	return repository.ListUsers()
}

// Login finds the user and creates a JWT for them.
// DEMO ONLY: there is no password. A real app checks the password (or SSO) here.
// This one combines two steps (find user + create token): a typical service job.
func Login(email string) (string, models.User, error) {
	user, err := repository.FindUserByEmail(email)
	if err != nil {
		return "", user, err
	}

	token, err := auth.CreateToken(user)
	if err != nil {
		return "", user, err
	}
	return token, user, nil
}
