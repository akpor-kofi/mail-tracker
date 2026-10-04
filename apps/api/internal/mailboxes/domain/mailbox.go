package domain

import "time"

type Mailbox struct {
	GrantedScopes []string
	SyncEnabled   bool
	ID            string
	OwnerID       string
	GoogleSub     string
	Email         string
	ConnectedAt   time.Time
}
