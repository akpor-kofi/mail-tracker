package domain

import (
	"errors"
	"net/mail"
	"strings"
)

type Draft struct {
	ID               string   `json:"id,omitempty"`
	MailboxID        string   `json:"mailboxId"`
	To               []string `json:"to"`
	Cc               []string `json:"cc"`
	Bcc              []string `json:"bcc"`
	Subject          string   `json:"subject"`
	HTML             string   `json:"html"`
	ReplyToMessageID string   `json:"replyToMessageId,omitempty"`
	ThreadID         string   `json:"threadId,omitempty"`
	Attachments      []string `json:"attachments,omitempty"`
}
type PlannedDelivery struct {
	Recipients []string
	To         []string
	Cc         []string
	Bcc        []string
}

func Plan(d Draft, mode string) ([]PlannedDelivery, error) {
	if d.MailboxID == "" || strings.TrimSpace(d.Subject) == "" || len(d.To) == 0 {
		return nil, errors.New("mailbox, subject, and To recipient are required")
	}
	for _, address := range append(append(append([]string{}, d.To...), d.Cc...), d.Bcc...) {
		parsed, err := mail.ParseAddress(address)
		if err != nil || parsed.Address != address {
			return nil, errors.New("invalid recipient address")
		}
	}
	if mode == "separate" {
		if len(d.Cc) > 0 || len(d.Bcc) > 0 {
			return nil, errors.New("separate sends cannot include Cc or Bcc")
		}
		out := make([]PlannedDelivery, 0, len(d.To))
		for _, to := range d.To {
			out = append(out, PlannedDelivery{Recipients: []string{to}, To: []string{to}})
		}
		return out, nil
	}
	if mode != "shared" {
		return nil, errors.New("invalid send mode")
	}
	all := append(append(append([]string{}, d.To...), d.Cc...), d.Bcc...)
	return []PlannedDelivery{{Recipients: all, To: d.To, Cc: d.Cc, Bcc: d.Bcc}}, nil
}
