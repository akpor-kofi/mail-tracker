package adapters

import (
	"context"
	"errors"
	"google.golang.org/api/idtoken"
)

type GoogleIdentity struct{ ClientID string }

func (g GoogleIdentity) Verify(ctx context.Context, raw string) (string, error) {
	if g.ClientID == "" {
		return "", errors.New("add-on client ID not configured")
	}
	p, err := idtoken.Validate(ctx, raw, g.ClientID)
	if err != nil {
		return "", err
	}
	return p.Subject, nil
}
