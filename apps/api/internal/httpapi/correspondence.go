package httpapi

import (
	"context"
	"errors"

	corrdb "github.com/akpor-kofi/mail-tracker/apps/api/internal/correspondence/adapters"
	corrapp "github.com/akpor-kofi/mail-tracker/apps/api/internal/correspondence/application"
	corrdomain "github.com/akpor-kofi/mail-tracker/apps/api/internal/correspondence/domain"
	corrapi "github.com/akpor-kofi/mail-tracker/apps/api/internal/httpapi/correspondence"
	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"
)

func draftIn(d corrapi.DraftInput) corrdomain.Draft {
	return corrdomain.Draft{ID: str(d.Id), MailboxID: d.MailboxId, To: d.To, Cc: d.Cc, Bcc: d.Bcc, Subject: d.Subject, HTML: d.Html, ReplyToMessageID: str(d.ReplyToMessageId), ThreadID: str(d.ThreadId), Attachments: func() []string {
		if d.Attachments == nil {
			return nil
		}
		return *d.Attachments
	}()}
}

func draftOut(d corrdb.DraftRecord) corrapi.Draft {
	return corrapi.Draft{Id: d.ID, MailboxId: d.Content.MailboxID, To: d.Content.To, Cc: d.Content.Cc, Bcc: d.Content.Bcc, Subject: d.Content.Subject, Html: d.Content.HTML, ReplyToMessageId: ptr(d.Content.ReplyToMessageID), ThreadId: ptr(d.Content.ThreadID), Attachments: &d.Content.Attachments, UpdatedAt: d.UpdatedAt}
}

func deliveryOut(d corrapp.Delivery) corrapi.Delivery {
	return corrapi.Delivery{Id: d.ID, Recipients: d.Recipients, ReplyAllRecipients: d.ReplyAllRecipients, Status: corrapi.DeliveryStatus(d.Status), Error: safeDeliveryError(d.Status, d.Error), GmailMessageId: ptr(d.GmailMessageID), GmailThreadId: ptr(d.GmailThreadID), RfcMessageId: ptr(d.RFCMessageID)}
}

func (s *Server) ListDrafts(ctx context.Context, _ corrapi.ListDraftsRequestObject) (corrapi.ListDraftsResponseObject, error) {
	ds, err := s.Correspondence.ListDrafts(ctx, owner(ctx))
	if err != nil {
		return nil, err
	}
	out := corrapi.ListDrafts200JSONResponse{}
	for _, d := range ds {
		out = append(out, draftOut(d))
	}
	return out, nil
}

func (s *Server) GetDraft(ctx context.Context, r corrapi.GetDraftRequestObject) (corrapi.GetDraftResponseObject, error) {
	d, err := s.Correspondence.GetDraft(ctx, owner(ctx), r.DraftId)
	if err != nil {
		return nil, err
	}
	return corrapi.GetDraft200JSONResponse(draftOut(d)), nil
}

func (s *Server) SaveDraft(ctx context.Context, r corrapi.SaveDraftRequestObject) (corrapi.SaveDraftResponseObject, error) {
	if r.Body == nil {
		return nil, fiber.NewError(400, "missing draft")
	}
	d, err := s.Correspondence.SaveDraft(ctx, owner(ctx), str(r.Body.Id), draftIn(*r.Body))
	if err != nil {
		if errors.Is(err, corrdb.ErrDraftAttachment) {
			return nil, fiber.NewError(400, corrdb.ErrDraftAttachment.Error())
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fiber.NewError(400, "mailbox or draft not found")
		}
		return nil, internalError("save draft", err)
	}
	return corrapi.SaveDraft200JSONResponse(draftOut(d)), nil
}

func (s *Server) DeleteDraft(ctx context.Context, r corrapi.DeleteDraftRequestObject) (corrapi.DeleteDraftResponseObject, error) {
	if err := s.Correspondence.DeleteDraft(ctx, owner(ctx), r.DraftId); err != nil {
		return nil, err
	}
	return corrapi.DeleteDraft204Response{}, nil
}

func (s *Server) SendTrackedMessage(ctx context.Context, r corrapi.SendTrackedMessageRequestObject) (corrapi.SendTrackedMessageResponseObject, error) {
	if r.Body == nil {
		return nil, fiber.NewError(400, "missing request")
	}
	result, err := s.Send.Send(ctx, owner(ctx), r.Body.IdempotencyKey, string(r.Body.SendMode), draftIn(r.Body.Draft))
	if err != nil {
		var conflict *corrapp.IdempotencyConflict
		if errors.As(err, &conflict) {
			return nil, fiber.NewError(409, conflict.Error())
		}
		var validation *corrapp.ValidationError
		if errors.As(err, &validation) {
			return nil, fiber.NewError(400, validation.Error())
		}
		return nil, internalError("send tracked message", err)
	}
	out := corrapi.SendTrackedMessage200JSONResponse{ConversationId: result.ConversationID, Deliveries: []corrapi.Delivery{}}
	for _, d := range result.Deliveries {
		out.Deliveries = append(out.Deliveries, deliveryOut(d))
	}
	return out, nil
}
