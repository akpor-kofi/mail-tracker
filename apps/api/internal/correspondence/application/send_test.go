package application

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/akpor-kofi/mail-tracker/apps/api/internal/correspondence/domain"
)

type fakeRepo struct {
	mu        sync.Mutex
	result    Result
	existing  bool
	updates   []string
	deleted   bool
	finished  chan struct{}
	deletedCh chan struct{}
}

func (f *fakeRepo) CreateAttempt(_ context.Context, _ string, _ string, _ domain.Draft, _ string, ds []Delivery) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.existing {
		return f.result.ConversationID, true, nil
	}
	f.result = Result{ConversationID: "conversation", Deliveries: append([]Delivery(nil), ds...)}
	f.finished = make(chan struct{})
	f.deletedCh = make(chan struct{})
	return "conversation", false, nil
}
func (f *fakeRepo) GetResult(_ context.Context, _ string, _ string) (Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return Result{ConversationID: f.result.ConversationID, Deliveries: append([]Delivery(nil), f.result.Deliveries...)}, nil
}
func (f *fakeRepo) UpdateDelivery(_ context.Context, id, status string, sent SentMessage, problem string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, status)
	for i := range f.result.Deliveries {
		if f.result.Deliveries[i].ID == id {
			f.result.Deliveries[i].Status = status
			f.result.Deliveries[i].Error = problem
			f.result.Deliveries[i].GmailMessageID = sent.GmailID
		}
	}
	if len(f.updates) == len(f.result.Deliveries) {
		close(f.finished)
	}
	return nil
}
func (f *fakeRepo) DeleteDraft(context.Context, string, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = true
	close(f.deletedCh)
	return nil
}

func waitFor(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("background send did not finish")
	}
}

type fakeSender struct {
	calls    atomic.Int32
	failures map[string]error
}

func (f *fakeSender) Send(_ context.Context, _ string, _ domain.Draft, p domain.PlannedDelivery, _ string) (SentMessage, error) {
	f.calls.Add(1)
	if err := f.failures[p.Recipients[0]]; err != nil {
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
	sender := &fakeSender{failures: map[string]error{"b@example.com": errors.New("rejected"), "c@example.com": timeoutError{}}}
	service := Service{Repo: repo, Sender: sender, PublicURL: "https://example.com"}
	d := domain.Draft{MailboxID: "m", To: []string{"a@example.com", "b@example.com", "c@example.com"}, Subject: "Hi"}
	result, err := service.Send(context.Background(), "owner", "key", "separate", d)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Deliveries) != 3 || result.Deliveries[0].Status != "pending" {
		t.Fatalf("send did not return pending deliveries: %+v", result)
	}
	waitFor(t, repo.finished)
	if sender.calls.Load() != 3 {
		t.Fatalf("unexpected delivery count: %+v", result)
	}
	recorded, _ := repo.GetResult(context.Background(), "owner", "conversation")
	if recorded.Deliveries[0].Status != "sent" || recorded.Deliveries[1].Status != "failed" || recorded.Deliveries[2].Status != "unknown" {
		t.Fatalf("wrong states: %+v", recorded.Deliveries)
	}
	if recorded.Deliveries[1].Error == "rejected" || recorded.Deliveries[2].Error == "connection timed out" {
		t.Fatalf("adapter error leaked into delivery records: %+v", recorded.Deliveries)
	}
	repo.mu.Lock()
	repo.existing = true
	repo.mu.Unlock()
	_, err = service.Send(context.Background(), "owner", "key", "separate", d)
	if err != nil {
		t.Fatal(err)
	}
	if sender.calls.Load() != 3 {
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
	waitFor(t, repo.deletedCh)
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
	waitFor(t, repo.finished)
}

type blockingSender struct {
	active  atomic.Int32
	peak    atomic.Int32
	started chan struct{}
	release chan struct{}
}

func (s *blockingSender) Send(_ context.Context, _ string, _ domain.Draft, _ domain.PlannedDelivery, _ string) (SentMessage, error) {
	n := s.active.Add(1)
	for {
		peak := s.peak.Load()
		if n <= peak || s.peak.CompareAndSwap(peak, n) {
			break
		}
	}
	s.started <- struct{}{}
	<-s.release
	s.active.Add(-1)
	return SentMessage{GmailID: "gmail"}, nil
}

func TestSendReturnsBeforeBoundedWorkersFinish(t *testing.T) {
	repo := &fakeRepo{}
	sender := &blockingSender{started: make(chan struct{}, 8), release: make(chan struct{})}
	service := Service{Repo: repo, Sender: sender, PublicURL: "https://example.com", Slots: make(chan struct{}, 4)}
	d := domain.Draft{MailboxID: "m", Subject: "Hi"}
	for i := 0; i < 8; i++ {
		d.To = append(d.To, string(rune('a'+i))+"@example.com")
	}
	result, err := service.Send(context.Background(), "owner", "key", "separate", d)
	if err != nil || len(result.Deliveries) != 8 || result.Deliveries[0].Status != "pending" {
		t.Fatalf("unexpected accepted result: %+v, %v", result, err)
	}
	for i := 0; i < 4; i++ {
		select {
		case <-sender.started:
		case <-time.After(3 * time.Second):
			t.Fatal("workers did not start")
		}
	}
	if sender.peak.Load() > 4 {
		t.Fatalf("started %d concurrent sends", sender.peak.Load())
	}
	close(sender.release)
	waitFor(t, repo.finished)
	if sender.peak.Load() > 4 {
		t.Fatalf("ran %d concurrent sends", sender.peak.Load())
	}
}

func TestMissingSendPermissionIsDefiniteFailureAndKeepsDraft(t *testing.T) {
	repo := &fakeRepo{}
	sender := &fakeSender{failures: map[string]error{"a@example.com": ErrSendPermission}}
	service := Service{Repo: repo, Sender: sender, PublicURL: "https://example.com"}
	d := domain.Draft{ID: "draft", MailboxID: "m", To: []string{"a@example.com"}, Subject: "Hi"}
	if _, err := service.Send(context.Background(), "owner", "key", "shared", d); err != nil {
		t.Fatal(err)
	}
	waitFor(t, repo.finished)
	recorded, _ := repo.GetResult(context.Background(), "owner", "conversation")
	if recorded.Deliveries[0].Status != "failed" || recorded.Deliveries[0].Error != SendPermissionError {
		t.Fatalf("wrong permission recovery: %+v", recorded)
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if sender.calls.Load() != 1 || repo.deleted {
		t.Fatal("permission failure retried send or removed draft")
	}
}
