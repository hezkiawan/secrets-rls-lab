// Package openbao is the ONLY place in the API that talks to OpenBao.
//
// Every function here is one HTTP request to OpenBao. The CLI command that sends the
// same request is written above each function, so you can try it by hand.
package openbao

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/openbao/openbao/api/auth/approle/v2"
	"github.com/openbao/openbao/api/v2"
)

// client is shared by all functions in this file. After Login it carries our token,
// and the library sends that token with every request (header X-Vault-Token).
var client *api.Client

// Connect creates the client. It does not send anything yet.
func Connect(address string) error {
	config := api.DefaultConfig()
	config.Address = address

	newClient, err := api.NewClient(config)
	if err != nil {
		return err
	}
	client = newClient
	return nil
}

// Login logs in with AppRole and keeps the token inside the client.
//
//	CLI:  bao write auth/approle/login role_id=... secret_id=...
//	HTTP: PUT /v1/auth/approle/login
func Login(roleIDFile string, secretIDFile string) error {
	roleIDBytes, err := os.ReadFile(roleIDFile)
	if err != nil {
		return err
	}
	roleID := strings.TrimSpace(string(roleIDBytes)) // remove the newline at the end

	appRole, err := approle.NewAppRoleAuth(roleID, &approle.SecretID{FromFile: secretIDFile})
	if err != nil {
		return err
	}

	result, err := client.Auth().Login(context.Background(), appRole)
	if err != nil {
		return err
	}
	if result == nil || result.Auth == nil {
		return errors.New("login returned no token")
	}
	return nil
}

// RenewToken asks OpenBao to extend our token. Returns the seconds it is now valid for.
//
//	CLI:  bao token renew
//	HTTP: PUT /v1/auth/token/renew-self
func RenewToken() (int, error) {
	result, err := client.Auth().Token().RenewSelf(0) // 0 = "the normal period"
	if err != nil {
		return 0, err
	}
	return result.Auth.LeaseDuration, nil
}

// AppSecrets are the app's static secrets (what used to be in .env).
type AppSecrets struct {
	JWTSigningKey string
	MetaAPIToken  string
}

// ReadAppSecrets reads secret/<product>/app from the KV engine.
//
//	CLI:  bao kv get secret/kouventa/app
//	HTTP: GET /v1/secret/data/kouventa/app
func ReadAppSecrets(product string) (AppSecrets, error) {
	var secrets AppSecrets

	result, err := client.KVv2("secret").Get(context.Background(), product+"/app")
	if err != nil {
		return secrets, err
	}

	// result.Data is a map. We check each value really is a string.
	jwtKey, ok := result.Data["jwt_signing_key"].(string)
	if !ok {
		return secrets, errors.New("jwt_signing_key is missing")
	}
	metaToken, ok := result.Data["meta_api_token"].(string)
	if !ok {
		return secrets, errors.New("meta_api_token is missing")
	}

	secrets.JWTSigningKey = jwtKey
	secrets.MetaAPIToken = metaToken
	return secrets, nil
}

// DBCredentials is one temporary Postgres user created by OpenBao.
type DBCredentials struct {
	Username     string
	Password     string
	LeaseID      string // needed to renew this user later
	LeaseSeconds int    // how long it is valid right now
}

// GetDBCredentials asks OpenBao for a NEW temporary Postgres user.
// OpenBao runs CREATE ROLE "v-approle-..." in Postgres at this moment.
//
//	CLI:  bao read database/creds/kouventa-app
//	HTTP: GET /v1/database/creds/kouventa-app
func GetDBCredentials(role string) (DBCredentials, error) {
	var creds DBCredentials

	result, err := client.Logical().Read("database/creds/" + role)
	if err != nil {
		return creds, err
	}
	if result == nil {
		return creds, errors.New("no credentials returned")
	}

	username, ok := result.Data["username"].(string)
	if !ok {
		return creds, errors.New("username is missing")
	}
	password, ok := result.Data["password"].(string)
	if !ok {
		return creds, errors.New("password is missing")
	}

	creds.Username = username
	creds.Password = password
	creds.LeaseID = result.LeaseID
	creds.LeaseSeconds = result.LeaseDuration
	return creds, nil
}

// RenewDBCredentials extends the lease of a temporary DB user.
// Returns the seconds it is now valid for (this gets smaller near its maximum lifetime).
//
//	CLI:  bao lease renew <lease_id>
//	HTTP: PUT /v1/sys/leases/renew
func RenewDBCredentials(leaseID string) (int, error) {
	result, err := client.Sys().Renew(leaseID, 0)
	if err != nil {
		return 0, err
	}
	return result.LeaseDuration, nil
}

// ReadDBOwnerLogin reads the table OWNER's login from KV.
// LAB ONLY: used by the "as owner" RLS demo. A real app must never have this.
//
//	CLI:  bao kv get secret/kouventa/db-owner
func ReadDBOwnerLogin(product string) (string, string, error) {
	result, err := client.KVv2("secret").Get(context.Background(), product+"/db-owner")
	if err != nil {
		return "", "", err
	}
	username, _ := result.Data["username"].(string)
	password, _ := result.Data["password"].(string)
	return username, password, nil
}

// ActiveNode returns the address of the cluster's current leader (for /status).
//
//	CLI:  bao status   (the "Active Node Address" line)
//	HTTP: GET /v1/sys/leader
func ActiveNode() string {
	result, err := client.Sys().Leader()
	if err != nil {
		return "unknown"
	}
	return result.LeaderAddress
}
