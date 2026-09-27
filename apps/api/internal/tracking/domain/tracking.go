package domain

import "time"

type Conversation struct {
	ID, MailboxID, Subject, Status, OpenStatus string
	UpdatedAt                                  time.Time
}
type Delivery struct {
	ID                            string
	Recipients                    []string
	Status, Error, GmailMessageID string
	GmailThreadID                 string
	RFCMessageID                  string
}
type Event struct {
	DeliveryID string
	At         time.Time
}
type Detail struct {
	Conversation Conversation
	Deliveries   []Delivery
	Events       []Event
}
