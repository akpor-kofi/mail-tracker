package domain

import "time"

type Mailbox struct {
	ID          string
	OwnerID     string
	GoogleSub   string
	Email       string
	ConnectedAt time.Time
}
