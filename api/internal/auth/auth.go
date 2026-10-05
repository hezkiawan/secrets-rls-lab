// Package auth issues and verifies the demo JWTs.
//
// The signing key comes from OpenBao (secret/kouventa/app → jwt_signing_key), loaded in M1.
// DEMO ONLY: /login accepts just an email (no password). Real login is out of scope;
// what matters for RLS is what happens AFTER login: the token says who you are.
package auth

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/golang-jwt/jwt/v5"

	"secrets-rls-lab/api/internal/db"
)

// claims is what we put inside the token.
type claims struct {
	CompanyID string `json:"company_id"`
	Role      string `json:"role"`
	Name      string `json:"name"`
	jwt.RegisteredClaims
}

// Issuer signs and verifies tokens with one HMAC key.
type Issuer struct {
	key []byte
}

// NewIssuer creates an Issuer. key must be the secret from OpenBao.
func NewIssuer(key string) *Issuer { return &Issuer{key: []byte(key)} }

// Issue creates a signed token for who, valid for one hour.
func (i *Issuer) Issue(who db.Who) (string, error) {
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		CompanyID: who.CompanyID,
		Role:      who.Role,
		Name:      who.Name,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   who.UserID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		},
	})
	return token.SignedString(i.key)
}

// Verify checks the signature and expiry and returns who the token belongs to.
func (i *Issuer) Verify(raw string) (db.Who, error) {
	var c claims
	_, err := jwt.ParseWithClaims(raw, &c,
		func(*jwt.Token) (any, error) { return i.key, nil },
		jwt.WithValidMethods([]string{"HS256"}), // never accept "alg: none" or other algorithms
	)
	if err != nil {
		return db.Who{}, err
	}
	if c.Subject == "" || c.CompanyID == "" {
		return db.Who{}, errors.New("token is missing user or company")
	}
	return db.Who{UserID: c.Subject, CompanyID: c.CompanyID, Role: c.Role, Name: c.Name}, nil
}

const whoKey = "who"

// Require is Fiber middleware: it rejects requests without a valid
// "Authorization: Bearer <token>" header, and stores Who for the handler.
func (i *Issuer) Require() fiber.Handler {
	return func(c fiber.Ctx) error {
		header := c.Get(fiber.HeaderAuthorization)
		raw, ok := strings.CutPrefix(header, "Bearer ")
		if !ok || raw == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "missing Bearer token — log in first"})
		}
		who, err := i.Verify(raw)
		if err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": fmt.Sprintf("invalid token: %v", err)})
		}
		c.Locals(whoKey, who)
		return c.Next()
	}
}

// WhoFrom returns the Who stored by Require. Only call it on routes that use Require.
func WhoFrom(c fiber.Ctx) db.Who {
	who, _ := c.Locals(whoKey).(db.Who)
	return who
}
