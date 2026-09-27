package application

import (
	"context"
	"sync"
	"time"

	"github.com/akpor-kofi/mail-tracker/apps/api/internal/mailboxes/domain"
	"golang.org/x/oauth2"
	"golang.org/x/sync/singleflight"
)

type cachedCredentials struct {
	mailbox domain.Mailbox
	token   oauth2.Token
}

type failedRefresh struct {
	err   error
	until time.Time
}

// AccessTokenCache shares one refresh per mailbox and keeps access tokens only
// while they have at least a minute of validity left.
type AccessTokenCache struct {
	mu       sync.RWMutex
	entries  map[string]cachedCredentials
	failures map[string]failedRefresh
	refresh  singleflight.Group
}

func (c *AccessTokenCache) get(id string) (cachedCredentials, bool) {
	c.mu.RLock()
	entry, ok := c.entries[id]
	c.mu.RUnlock()
	return entry, ok && entry.token.AccessToken != "" && time.Until(entry.token.Expiry) > time.Minute
}

func (c *AccessTokenCache) put(id string, entry cachedCredentials) {
	if entry.token.AccessToken == "" || time.Until(entry.token.Expiry) <= time.Minute {
		return
	}
	c.mu.Lock()
	if c.entries == nil {
		c.entries = make(map[string]cachedCredentials)
	}
	c.entries[id] = entry
	delete(c.failures, id)
	c.mu.Unlock()
}

func (c *AccessTokenCache) failed(id string) (error, bool) {
	c.mu.RLock()
	failure, ok := c.failures[id]
	c.mu.RUnlock()
	return failure.err, ok && time.Now().Before(failure.until)
}

func (c *AccessTokenCache) fail(id string, err error) {
	c.mu.Lock()
	if c.failures == nil {
		c.failures = make(map[string]failedRefresh)
	}
	c.failures[id] = failedRefresh{err: err, until: time.Now().Add(5 * time.Second)}
	c.mu.Unlock()
}

func (c *AccessTokenCache) forget(id string) {
	c.mu.Lock()
	delete(c.entries, id)
	delete(c.failures, id)
	c.mu.Unlock()
	c.refresh.Forget(id)
}

func (c *AccessTokenCache) forgetGoogleSub(sub string) {
	c.mu.Lock()
	for id, entry := range c.entries {
		if entry.mailbox.GoogleSub == sub {
			delete(c.entries, id)
			c.refresh.Forget(id)
		}
	}
	clear(c.failures)
	c.mu.Unlock()
}

func (s OAuthService) Credentials(ctx context.Context, id string) (domain.Mailbox, *oauth2.Token, error) {
	if s.Cache == nil {
		entry, err := s.loadCredentials(ctx, id)
		return entry.mailbox, &entry.token, err
	}
	if entry, ok := s.Cache.get(id); ok {
		return entry.mailbox, &entry.token, nil
	}
	if err, ok := s.Cache.failed(id); ok {
		return domain.Mailbox{}, nil, err
	}
	result := s.Cache.refresh.DoChan(id, func() (any, error) {
		if entry, ok := s.Cache.get(id); ok {
			return entry, nil
		}
		if err, ok := s.Cache.failed(id); ok {
			return nil, err
		}
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		entry, err := s.loadCredentials(fetchCtx, id)
		if err == nil {
			s.Cache.put(id, entry)
		} else {
			s.Cache.fail(id, err)
		}
		return entry, err
	})
	select {
	case <-ctx.Done():
		return domain.Mailbox{}, nil, ctx.Err()
	case refreshed := <-result:
		if refreshed.Err != nil {
			return domain.Mailbox{}, nil, refreshed.Err
		}
		entry := refreshed.Val.(cachedCredentials)
		return entry.mailbox, &entry.token, nil
	}
}

func (s OAuthService) ForgetToken(id string) {
	if s.Cache != nil {
		s.Cache.forget(id)
	}
}
