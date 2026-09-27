package application

import (
	"context"
	"github.com/akpor-kofi/mail-tracker/apps/api/internal/tracking/domain"
)

type Repository interface {
	RecordOpen(context.Context, []byte) (string, error)
	List(context.Context, string, string) ([]domain.Conversation, error)
	Detail(context.Context, string, string) (domain.Detail, error)
	Prepare(context.Context, string, string, string, []string, []byte) (string, error)
}
type Events interface {
	Publish(string)
	Subscribe(string) (<-chan string, func())
}
