package httpapi

import (
	"context"

	mailboxapi "github.com/akpor-kofi/mail-tracker/apps/api/internal/httpapi/mailboxes"
)

func (s *Server) ListMailboxes(ctx context.Context, _ mailboxapi.ListMailboxesRequestObject) (mailboxapi.ListMailboxesResponseObject, error) {
	ms, err := s.MailboxRepo.List(ctx, owner(ctx))
	if err != nil {
		return nil, err
	}
	out := mailboxapi.ListMailboxes200JSONResponse{}
	for _, m := range ms {
		out = append(out, mailboxapi.Mailbox{Id: m.ID, Email: m.Email, ConnectedAt: m.ConnectedAt})
	}
	return out, nil
}

func (s *Server) StartMailboxConnect(ctx context.Context, _ mailboxapi.StartMailboxConnectRequestObject) (mailboxapi.StartMailboxConnectResponseObject, error) {
	url, err := s.Mailbox.Start(ctx, owner(ctx))
	if err != nil {
		return nil, err
	}
	return mailboxapi.StartMailboxConnect200JSONResponse{Url: url}, nil
}

func (s *Server) RemoveMailbox(ctx context.Context, r mailboxapi.RemoveMailboxRequestObject) (mailboxapi.RemoveMailboxResponseObject, error) {
	if err := s.MailboxRepo.Delete(ctx, owner(ctx), r.MailboxId); err != nil {
		return nil, err
	}
	s.Mailbox.ForgetToken(r.MailboxId)
	return mailboxapi.RemoveMailbox204Response{}, nil
}
