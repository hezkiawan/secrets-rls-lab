// Package auth creates and checks the user's login token (a JWT).
//
// The token is SIGNED with a key that comes from OpenBao (secret/kouventa/app,
// jwt_signing_key). Anyone who changes the token breaks the signature.
package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"secrets-rls-lab/api/models"
)

// signingKey is set once at startup (main.go), with the key read from OpenBao.
var signingKey []byte

func SetSigningKey(key string) {
	signingKey = []byte(key)
}

// CreateToken makes a signed token that says who the user is. Valid for 1 hour.
func CreateToken(user models.User) (string, error) {
	claims := jwt.MapClaims{
		"sub":        user.ID,
		"company_id": user.CompanyID,
		"role":       user.Role,
		"name":       user.Name,
		"exp":        time.Now().Add(time.Hour).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(signingKey)
}

// ParseToken checks the signature and the expiry time, then returns the user inside.
func ParseToken(tokenString string) (models.User, error) {
	var user models.User

	token, err := jwt.Parse(tokenString,
		func(t *jwt.Token) (interface{}, error) { return signingKey, nil },
		jwt.WithValidMethods([]string{"HS256"}), // only accept our signing method
	)
	if err != nil {
		return user, err
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return user, errors.New("unexpected token content")
	}

	user.ID, _ = claims["sub"].(string)
	user.CompanyID, _ = claims["company_id"].(string)
	user.Role, _ = claims["role"].(string)
	user.Name, _ = claims["name"].(string)

	if user.ID == "" || user.CompanyID == "" {
		return user, errors.New("token has no user or company")
	}
	return user, nil
}
