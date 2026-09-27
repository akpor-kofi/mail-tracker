package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"time"

	"github.com/akpor-kofi/mail-tracker/apps/api/internal/mailboxes/domain"
)

type PairingRepository interface {
	CreateCode(context.Context, []byte, string, time.Time) error
	ConsumeCode(context.Context, []byte, string) (string, error)
	SavePair(context.Context, string, string) error
	PairedMailbox(context.Context, string) (string, error)
	Mailbox(context.Context, string) (domain.Mailbox, error)
}
type IdentityVerifier interface {
	Verify(context.Context, string) (string, error)
}
type AddonService struct {
	Pairs     PairingRepository
	Tracking  Repository
	Identity  IdentityVerifier
	PublicURL string
	Events    Events
}

func (s AddonService) Code(ctx context.Context, mailboxID string) (string, time.Time, error) {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return "", time.Time{}, err
	}
	code := base64.RawURLEncoding.EncodeToString(b)
	hash := sha256.Sum256([]byte(code))
	expiry := time.Now().Add(10 * time.Minute)
	return code, expiry, s.Pairs.CreateCode(ctx, hash[:], mailboxID, expiry)
}
func (s AddonService) Pair(ctx context.Context, code, token string) (string, error) {
	sub, err := s.Identity.Verify(ctx, token)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256([]byte(code))
	mailboxID, err := s.Pairs.ConsumeCode(ctx, hash[:], sub)
	if err != nil {
		return "", err
	}
	if err := s.Pairs.SavePair(ctx, sub, mailboxID); err != nil {
		return "", err
	}
	return mailboxID, nil
}
func (s AddonService) Prepare(ctx context.Context, token, subject string, recipients []string) (string, string, error) {
	sub, err := s.Identity.Verify(ctx, token)
	if err != nil {
		return "", "", err
	}
	mailboxID, err := s.Pairs.PairedMailbox(ctx, sub)
	if err != nil {
		return "", "", err
	}
	m, err := s.Pairs.Mailbox(ctx, mailboxID)
	if err != nil {
		return "", "", err
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	pixel := base64.RawURLEncoding.EncodeToString(b)
	hash := sha256.Sum256([]byte(pixel))
	cid, err := s.Tracking.Prepare(ctx, m.OwnerID, mailboxID, subject, recipients, hash[:])
	if err != nil {
		return "", "", err
	}
	if s.Events != nil {
		s.Events.Publish(m.OwnerID)
	}
	return cid, s.PublicURL + "/p/" + pixel + ".gif", nil
}
