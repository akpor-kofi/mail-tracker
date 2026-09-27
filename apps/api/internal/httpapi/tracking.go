package httpapi

import (
	"context"
	"log"

	trackingapi "github.com/akpor-kofi/mail-tracker/apps/api/internal/httpapi/tracking"
	trackingdomain "github.com/akpor-kofi/mail-tracker/apps/api/internal/tracking/domain"
	"github.com/gofiber/fiber/v3"
)

func convOut(c trackingdomain.Conversation) trackingapi.Conversation {
	return trackingapi.Conversation{Id: c.ID, MailboxId: c.MailboxID, Subject: c.Subject, Status: c.Status, OpenStatus: trackingapi.ConversationOpenStatus(c.OpenStatus), UpdatedAt: c.UpdatedAt}
}

func detailOut(d trackingdomain.Detail) trackingapi.ConversationDetail {
	out := trackingapi.ConversationDetail{Id: d.Conversation.ID, MailboxId: d.Conversation.MailboxID, Subject: d.Conversation.Subject, Status: d.Conversation.Status, OpenStatus: trackingapi.ConversationDetailOpenStatus(d.Conversation.OpenStatus), UpdatedAt: d.Conversation.UpdatedAt, Deliveries: []trackingapi.Delivery{}, Events: []trackingapi.OpenEvent{}}
	for _, x := range d.Deliveries {
		out.Deliveries = append(out.Deliveries, trackingapi.Delivery{Id: x.ID, Recipients: x.Recipients, ReplyAllRecipients: x.ReplyAllRecipients, Status: trackingapi.DeliveryStatus(x.Status), Error: safeDeliveryError(x.Status, x.Error), GmailMessageId: ptr(x.GmailMessageID), GmailThreadId: ptr(x.GmailThreadID), RfcMessageId: ptr(x.RFCMessageID)})
	}
	for _, e := range d.Events {
		out.Events = append(out.Events, trackingapi.OpenEvent{DeliveryId: e.DeliveryID, At: e.At})
	}
	return out
}

func (s *Server) ListConversations(ctx context.Context, r trackingapi.ListConversationsRequestObject) (trackingapi.ListConversationsResponseObject, error) {
	cs, err := s.Tracking.List(ctx, owner(ctx), str(r.Params.MailboxId))
	if err != nil {
		return nil, err
	}
	out := trackingapi.ListConversations200JSONResponse{}
	for _, c := range cs {
		out = append(out, convOut(c))
	}
	return out, nil
}

func (s *Server) GetConversation(ctx context.Context, r trackingapi.GetConversationRequestObject) (trackingapi.GetConversationResponseObject, error) {
	d, err := s.Tracking.Detail(ctx, owner(ctx), r.ConversationId)
	if err != nil {
		return nil, err
	}
	return trackingapi.GetConversation200JSONResponse(detailOut(d)), nil
}

func (s *Server) CreatePairingCode(ctx context.Context, r trackingapi.CreatePairingCodeRequestObject) (trackingapi.CreatePairingCodeResponseObject, error) {
	if r.Body == nil {
		return nil, fiber.NewError(400, "missing mailbox")
	}
	m, _, err := s.MailboxRepo.Get(ctx, r.Body.MailboxId)
	if err != nil || m.OwnerID != owner(ctx) {
		return nil, fiber.NewError(404, "mailbox not found")
	}
	code, at, err := s.Addon.Code(ctx, m.ID)
	if err != nil {
		return nil, err
	}
	return trackingapi.CreatePairingCode200JSONResponse{Code: code, ExpiresAt: at}, nil
}

func (s *Server) PairAddon(ctx context.Context, r trackingapi.PairAddonRequestObject) (trackingapi.PairAddonResponseObject, error) {
	if r.Body == nil {
		return nil, fiber.NewError(400, "missing request")
	}
	id, err := s.Addon.Pair(ctx, r.Body.Code, r.Body.IdentityToken)
	if err != nil {
		log.Printf("pair add-on: %v", err)
		return nil, fiber.NewError(401, "invalid pairing code or identity")
	}
	return trackingapi.PairAddon200JSONResponse{MailboxId: id}, nil
}

func (s *Server) PrepareAddonDraft(ctx context.Context, r trackingapi.PrepareAddonDraftRequestObject) (trackingapi.PrepareAddonDraftResponseObject, error) {
	if r.Body == nil {
		return nil, fiber.NewError(400, "missing request")
	}
	cid, url, err := s.Addon.Prepare(ctx, r.Body.IdentityToken, r.Body.Subject, r.Body.Recipients)
	if err != nil {
		log.Printf("prepare add-on draft: %v", err)
		return nil, fiber.NewError(401, "add-on not paired or invalid identity")
	}
	return trackingapi.PrepareAddonDraft200JSONResponse{ConversationId: cid, PixelUrl: url}, nil
}
