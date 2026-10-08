// Package historyimport imports a profile's watch history from a Plex, Jellyfin or Emby server.
package historyimport

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/provider"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// requestTimeout bounds each request to a source, a page of a large library's included.
const requestTimeout = time.Minute

// ErrRefused is an import that cannot start: an address or credentials the source does not take,
// or a source that does not answer.
var ErrRefused = errors.New("import refused")

var client = &http.Client{Timeout: requestTimeout}

// Credentials sign in to a source: Plex by a token, Jellyfin and Emby by a user's name and
// password.
type Credentials struct {
	Token    string
	Username string
	Password string
}

// entry is a film or episode the source has watched or started.
type entry struct {
	title string
	kind  domain.ItemKind
	// ids are a film's own, or an episode's show's.
	ids             map[domain.Provider]string
	season, episode int
	plays           int
	position        time.Duration
	// at is when it was last played, zero where the source does not say.
	at time.Time
}

type source interface {
	// connect signs in, answering what the import signs in with as it runs.
	connect(ctx context.Context, c Credentials) (store.ImportLogin, error)
	entries(ctx context.Context) ([]entry, error)
	// signOut ends the session connect made, where it made one.
	signOut(ctx context.Context) error
}

func open(kind domain.ImportSource, base string, login store.ImportLogin) source {
	switch kind {
	case domain.ImportPlex:
		return plex{base: base, token: login.Token}
	case domain.ImportJellyfin:
		return jellyfin{kind: kind, base: base, login: login, header: "Authorization", items: "/Items"}
	case domain.ImportEmby:
		// Emby lists a user's items only under the user; Jellyfin 12 no longer does.
		return jellyfin{kind: kind, base: base, login: login, header: "X-Emby-Authorization", items: "/Users/{user}/Items"}
	}
	panic("historyimport: unknown source " + string(kind))
}

// connect signs in to a source, so an address or credentials it does not take refuse the import
// before it is queued.
func connect(ctx context.Context, kind domain.ImportSource, base string, c Credentials) (store.ImportLogin, error) {
	login, err := open(kind, base, store.ImportLogin{}).connect(ctx, c)
	if err == nil || errors.Is(err, ErrRefused) {
		return login, err
	}
	refusal, refused := errors.AsType[*provider.Refusal](err)
	switch {
	case ctx.Err() != nil:
		return login, ctx.Err()
	case errors.Is(err, provider.ErrUnreached):
		return login, fmt.Errorf("%w: the %s server did not answer: %w", ErrRefused, kind, err)
	case refused && (refusal.Code == http.StatusUnauthorized || refusal.Code == http.StatusForbidden):
		return login, fmt.Errorf("%w: the %s server refused the credentials", ErrRefused, kind)
	}
	return login, fmt.Errorf("%w: %w", ErrRefused, err)
}

// providerIDs keeps the ids a title is matched by, keyed in any case.
func providerIDs(all map[string]string) map[domain.Provider]string {
	out := map[domain.Provider]string{}
	for k, v := range all {
		p := domain.Provider(strings.ToLower(k))
		if v != "" && slices.Contains(domain.Providers(), p) {
			out[p] = v
		}
	}
	return out
}
