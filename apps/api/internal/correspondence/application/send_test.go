package application

import (
	"context"
	"errors"
	"testing"

	"github.com/akpor-kofi/mail-tracker/apps/api/internal/correspondence/domain"
)

type fakeRepo struct {
	result   Result
	existing bool
	updates  []string
	deleted  bool
}

func (f *fakeRepo) CreateAttempt(_ context.Context, _ string, _ string, _ domain.Draft, _ string, ds []Delivery) (string, bool, error) {
	if f.existing {
		return f.result.ConversationID, true, nil
	}
	f.result = Result{ConversationID: "conversation", Deliveries: ds}
	return "conversation", false, nil
}
func (f *fakeRepo) GetResult(_ context.Context, _ string, _ string) (Result, error) {
	return f.result, nil
}
func (f *fakeRepo) UpdateDelivery(_ context.Context, id, status string, sent SentMessage, problem string) error {
	f.updates = append(f.updates, status)
	for i := range f.result.Deliveries {
		if f.result.Deliveries[i].ID == id {
			f.result.Deliveries[i].Status = status
			f.result.Deliveries[i].Error = problem
			f.result.Deliveries[i].GmailMessageID = sent.GmailID
		}
	}
	return nil
}
func (f *fakeRepo) DeleteDraft(context.Context, string, string) error { f.deleted = true; return nil }

type fakeSender struct {
	calls    int
	failures map[int]error
}

func (f *fakeSender) Send(_ context.Context, _ string, _ domain.Draft, _ domain.PlannedDelivery, _ string) (SentMessage, error) {
	f.calls++
	if err := f.failures[f.calls]; err != nil {
		return SentMessage{}, err
	}
	return SentMessage{GmailID: "gmail"}, nil
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "connection timed out" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }
func TestSeparatePartialAndUnknown(t *testing.T) {
	repo := &fakeRepo{}
	sender := &fakeSender{failures: map[int]error{2: errors.New("rejected"), 3: timeoutError{}}}
	service := Service{Repo: repo, Sender: sender, PublicURL: "https://example.com"}
	d := domain.Draft{MailboxID: "m", To: []string{"a@example.com", "b@example.com", "c@example.com"}, Subject: "Hi"}
	result, err := service.Send(context.Background(), "owner", "key", "separate", d)
	if err != nil {
		t.Fatal(err)
	}
	if sender.calls != 3 || len(result.Deliveries) != 3 {
		t.Fatalf("unexpected delivery count: %+v", result)
	}
	if result.Deliveries[0].Status != "sent" || result.Deliveries[1].Status != "failed" || result.Deliveries[2].Status != "unknown" {
		t.Fatalf("wrong states: %+v", result.Deliveries)
	}
	repo.existing = true
	_, err = service.Send(context.Background(), "owner", "key", "separate", d)
	if err != nil {
		t.Fatal(err)
	}
	if sender.calls != 3 {
		t.Fatal("duplicate request sent another message")
	}
}
func TestConfirmedSendDeletesDraft(t *testing.T) {
	repo := &fakeRepo{}
	sender := &fakeSender{}
	service := Service{Repo: repo, Sender: sender, PublicURL: "https://example.com"}
	d := domain.Draft{ID: "draft", MailboxID: "m", To: []string{"a@example.com"}, Subject: "Hi"}
	_, err := service.Send(context.Background(), "owner", "key", "shared", d)
	if err != nil {
		t.Fatal(err)
	}
	if !repo.deleted {
		t.Fatal("confirmed draft was retained")
	}
}

func TestReplyAllExcludesBcc(t *testing.T) {
	repo := &fakeRepo{}
	sender := &fakeSender{}
	service := Service{Repo: repo, Sender: sender, PublicURL: "https://example.com"}
	d := domain.Draft{MailboxID: "m", To: []string{"to@example.com"}, Cc: []string{"cc@example.com"}, Bcc: []string{"private@example.com"}, Subject: "Hi"}
	result, err := service.Send(context.Background(), "owner", "key", "shared", d)
	if err != nil {
		t.Fatal(err)
	}
	got := result.Deliveries[0].ReplyAllRecipients
	if len(got) != 2 || got[0] != "to@example.com" || got[1] != "cc@example.com" {
		t.Fatalf("reply all leaked or omitted recipients: %v", got)
	}
}
