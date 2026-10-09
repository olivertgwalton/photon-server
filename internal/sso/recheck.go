package sso

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/olivertgwalton/photon-server/internal/store"
)

const (
	// RecheckEvery is how long an account at a provider that rechecks goes between asks: within
	// the hour or two the refresh tokens of Authelia and others last unused, which each ask renews.
	RecheckEvery = time.Hour
	// lookEvery is how often a node looks for accounts due to be asked after.
	lookEvery = time.Minute
)

// Run asks providers that recheck their accounts after each account an hour after it was last
// asked, or signed in, until ctx ends. Every node runs it; each account is asked by one.
func (s *Service) Run(ctx context.Context) {
	tick := time.NewTicker(lookEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := s.recheckDue(ctx, RecheckEvery); err != nil && ctx.Err() == nil {
				s.log.WarnContext(ctx, "sign-in accounts not rechecked", slog.Any("err", err))
			}
		}
	}
}

// recheckDue asks after the accounts last asked longer ago than every.
func (s *Service) recheckDue(ctx context.Context, every time.Duration) error {
	due, err := s.st.SignInChecksDue(ctx, every)
	if err != nil {
		return err
	}
	for _, c := range due {
		if err := s.recheck(ctx, c); err != nil && ctx.Err() == nil {
			// Asked again an hour on: a provider that is down signs no one out.
			s.log.WarnContext(ctx, "sign-in account not rechecked", slog.String("provider", c.Provider), slog.Any("err", err))
		}
	}
	return nil
}

// recheck asks an account's provider for new tokens by the refresh token it granted last. One it
// refuses, or whose claims say it has left the provider's group, signs its devices out; any other
// failure leaves it until it is asked again.
func (s *Service) recheck(ctx context.Context, c store.SignInCheck) error {
	p, err := s.st.SignInProvider(ctx, c.Provider)
	if errors.Is(err, store.ErrNotFound) {
		// Removed meanwhile, its accounts with it.
		return nil
	}
	if err != nil {
		return err
	}
	d, err := s.discover(ctx, p.Issuer)
	if err != nil {
		return err
	}
	tok, err := config(p, d, "").TokenSource(s.ask(ctx), &oauth2.Token{RefreshToken: c.Token}).Token()
	if accountRefused(err) {
		return s.signOut(ctx, c, err)
	}
	if err != nil {
		return err
	}
	if p.Group != "" {
		// The provider answers the account's claims as they are now: Keycloak, authentik, Kanidm and
		// Pocket ID in a new ID token (OIDC Core §12.2), Authelia only by its userinfo.
		var id *oidc.IDToken
		if raw, _ := tok.Extra("id_token").(string); raw != "" {
			if id, err = d.provider.Verifier(&oidc.Config{ClientID: p.ClientID}).Verify(ctx, raw); err != nil {
				return fmt.Errorf("verifying the refreshed ID token: %w", err)
			}
			if id.Subject != c.Subject {
				return errors.New("the refreshed ID token is another account's")
			}
		}
		claims, complete, err := s.claims(ctx, d, c.Subject, id, tok)
		if err != nil {
			return err
		}
		if complete && !admitted(p, claims) {
			return s.signOut(ctx, c, fmt.Errorf("not in %q", p.Group))
		}
	}
	return s.st.RenewSignInToken(ctx, c, tok.RefreshToken)
}

// accountRefused is whether a provider turned a refresh token down for its account's sake: a 400 for a
// grant that is invalid, expired or revoked (RFC 6749 §5.2), or as Zitadel and Pocket ID say it of
// an account disabled or out of a client's groups. A provider that turns down the client an admin
// registered, fails, or is not reached has said nothing of the account.
func accountRefused(err error) bool {
	re, ok := errors.AsType[*oauth2.RetrieveError](err)
	if !ok || re.Response == nil || re.Response.StatusCode != http.StatusBadRequest {
		return false
	}
	switch re.ErrorCode {
	case "invalid_client", "unauthorized_client", "unsupported_grant_type", "invalid_scope":
		return false
	}
	return true
}

// signOut signs out the devices an account signed in, for why.
func (s *Service) signOut(ctx context.Context, c store.SignInCheck, why error) error {
	ended, err := s.st.EndSignInSessions(ctx, c)
	if ended {
		s.log.InfoContext(ctx, "an account's devices were signed out on its provider's word",
			slog.String("provider", c.Provider), slog.Any("why", why))
	}
	return err
}
