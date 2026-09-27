package application

import (
	"context"
	"github.com/akpor-kofi/mail-tracker/apps/api/internal/mailboxes/domain"
	"time"
)

type Repository interface {
	SaveState(context.Context, []byte, string, string, time.Time) error
	ConsumeState(context.Context, []byte) (string, string, error)
	Upsert(context.Context, domain.Mailbox, []byte) error
	UpdateRefreshToken(context.Context, string, []byte) error
	List(context.Context, string) ([]domain.Mailbox, error)
	Get(context.Context, string) (domain.Mailbox, []byte, error)
	Delete(context.Context, string, string) error
	FindByGoogleSub(context.Context, string) (domain.Mailbox, error)
}
