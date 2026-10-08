package main

// Background jobs. main.go starts each one with `go`, so they run forever next to
// the web server. Tokens and DB users EXPIRE on purpose; these jobs keep them alive.

import (
	"log"
	"time"

	"secrets-rls-lab/api/config"
	"secrets-rls-lab/api/database"
	"secrets-rls-lab/api/openbao"
)

// How often the jobs wake up. Our lab TTLs are 2 minutes, so 30 seconds is plenty.
const checkEvery = 30 * time.Second

// keepTokenAlive renews our OpenBao token every 30 seconds.
// Our AppRole gives a PERIODIC token: it can be renewed forever, as long as we renew
// it in time. If renewing fails (e.g. the API was paused too long), we log in again.
func keepTokenAlive(cfg config.Config) {
	for {
		time.Sleep(checkEvery)

		secondsLeft, err := openbao.RenewToken()
		if err == nil {
			log.Println("[token] renewed, valid for", secondsLeft, "seconds")
			continue
		}

		log.Println("[token] renew failed, logging in again:", err)
		err = openbao.Login(cfg.RoleIDFile, cfg.SecretIDFile)
		if err != nil {
			log.Println("[token] login failed, will retry:", err)
		}
	}
}

// keepDatabaseUserAlive renews our temporary DB user every 30 seconds.
// Each DB user has a MAXIMUM lifetime (6 minutes in the lab). When it gets close,
// renewing no longer helps, so we get a NEW user and switch to it:
//
//	new user from OpenBao → new pool → SetPool (old pool closes) → keep renewing the new one
func keepDatabaseUserAlive(cfg config.Config, creds openbao.DBCredentials) {
	for {
		time.Sleep(checkEvery)

		// 1. Try to renew the current user.
		secondsLeft, err := openbao.RenewDBCredentials(creds.LeaseID)
		if err != nil {
			log.Println("[db] renew failed:", err)
			secondsLeft = 0 // treat as "about to expire" → replace it below
		} else {
			log.Println("[db] user", creds.Username, "renewed, valid for", secondsLeft, "seconds")
		}

		// 2. Still more than a minute left? Then we're fine until the next check.
		if secondsLeft > 60 {
			continue
		}

		// 3. Close to the end → replace it with a new user.
		log.Println("[db] user is near its maximum lifetime, getting a new one")

		newCreds, err := openbao.GetDBCredentials(cfg.DBRole)
		if err != nil {
			log.Println("[db] could not get a new user, will retry:", err)
			continue
		}

		newPool, err := database.Connect(cfg.DBAddr, cfg.DBName, newCreds.Username, newCreds.Password)
		if err != nil {
			log.Println("[db] could not connect with the new user, will retry:", err)
			continue
		}

		database.SetPool(newPool, newCreds.Username) // requests now use the new pool
		creds = newCreds
		log.Println("[db] now using new user", creds.Username)
	}
}
