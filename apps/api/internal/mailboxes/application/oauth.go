package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"time"

	"github.com/akpor-kofi/mail-tracker/apps/api/internal/mailboxes/domain"
	platformcrypto "github.com/akpor-kofi/mail-tracker/apps/api/internal/platform/crypto"
	"github.com/google/uuid"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/idtoken"
)

type OAuthService struct {
	Repo                            Repository
	Sealer                          platformcrypto.Sealer
	ClientID, ClientSecret, BaseURL string
}

func (s OAuthService) Config() *oauth2.Config {
	return &oauth2.Config{ClientID: s.ClientID, ClientSecret: s.ClientSecret, RedirectURL: s.BaseURL + "/oauth/google/callback", Scopes: []string{"openid", "email", "https://www.googleapis.com/auth/gmail.send"}, Endpoint: google.Endpoint}
}
func random() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func (s OAuthService) Start(ctx context.Context, owner string) (string, error) {
	state, err := random()
	if err != nil {
		return "", err
	}
	verifier, err := random()
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256([]byte(state))
	if err := s.Repo.SaveState(ctx, hash[:], owner, verifier, time.Now().Add(10*time.Minute)); err != nil {
		return "", err
	}
	return s.Config().AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce, oauth2.S256ChallengeOption(verifier)), nil
}
func (s OAuthService) Complete(ctx context.Context, state, code string) (string, error) {
	hash := sha256.Sum256([]byte(state))
	owner, verifier, err := s.Repo.ConsumeState(ctx, hash[:])
	if err != nil {
		return "", errors.New("invalid or expired OAuth state")
	}
	token, err := s.Config().Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return "", err
	}
	if token.RefreshToken == "" {
		return "", errors.New("Google did not return an offline refresh token; revoke prior access and reconnect")
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		return "", errors.New("Google did not return an identity token")
	}
	identity, err := idtoken.Validate(ctx, raw, s.ClientID)
	if err != nil {
		return "", err
	}
	email, ok := identity.Claims["email"].(string)
	if !ok || email == "" {
		return "", errors.New("Google identity has no email")
	}
	verified, ok := identity.Claims["email_verified"].(bool)
	if !ok || !verified {
		return "", errors.New("Google email is not verified")
	}
	encrypted, err := s.Sealer.Seal(token.RefreshToken)
	if err != nil {
		return "", err
	}
	m := domain.Mailbox{ID: uuid.NewString(), OwnerID: owner, GoogleSub: identity.Subject, Email: email}
	if err := s.Repo.Upsert(ctx, m, encrypted); err != nil {
		return "", err
	}
	return email, nil
}
func (s OAuthService) Token(ctx context.Context, id string) (*oauth2.Token, error) {
	_, encrypted, err := s.Repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	refresh, err := s.Sealer.Open(encrypted)
	if err != nil {
		return nil, err
	}
	token, err := s.Config().TokenSource(ctx, &oauth2.Token{RefreshToken: refresh}).Token()
	if err != nil {
		return nil, err
	}
	if token.RefreshToken != "" && token.RefreshToken != refresh {
		encrypted, err := s.Sealer.Seal(token.RefreshToken)
		if err != nil {
			return nil, err
		}
		if err := s.Repo.UpdateRefreshToken(ctx, id, encrypted); err != nil {
			return nil, err
		}
	}
	return token, nil
}
