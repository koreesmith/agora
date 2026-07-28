package atproto

import (
	"context"
	"errors"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/xrpc"
)

var errNoBotAccountConfigured = errors.New("atproto: no bot account configured for authenticated Bluesky search")

// defaultBotPDSHost is the entryway almost every Bluesky account (including
// one created through the official app) is hosted on. An authenticated
// client talks to its own account's PDS host, not the anonymous AppView
// mirrors (api.bsky.app/public.api.bsky.app) every other call in this
// package uses — those two don't support authentication at all. The PDS
// transparently proxies appview-only lexicons (like app.bsky.feed.searchPosts)
// for an authenticated caller, the same way the official app's own client
// works. Overridable via instance_settings for an instance whose bot account
// lives on a different PDS.
const defaultBotPDSHost = "https://bsky.social"

// authedAppviewClient authenticates as the instance's own dedicated Bluesky
// bot account (AGORA-216) so SearchBlueskyPosts can call the one AT Proto
// endpoint that 403s for anonymous callers. A dedicated bot account — not a
// real user's — keeps this out of any one person's account and separate
// from Agora's own per-user repo-hosting responsibilities (internal/atproto's
// own DID/PDS work).
//
// Session tokens are cached in instance_settings (the same key-value table
// atproto_enabled/atproto_appview_host already use) so a login only happens
// once per access-token lifetime rather than once per search request.
func (s *Service) authedAppviewClient(ctx context.Context) (*xrpc.Client, error) {
	handle := s.cfg.ATProtoBotHandle
	password := s.cfg.ATProtoBotAppPassword
	if handle == "" || password == "" {
		return nil, errNoBotAccountConfigured
	}

	host := s.botPDSHost()
	accessJwt, refreshJwt, did := s.cachedBotSession()
	if accessJwt != "" {
		return &xrpc.Client{
			Host: host,
			Auth: &xrpc.AuthInfo{AccessJwt: accessJwt, RefreshJwt: refreshJwt, Did: did, Handle: handle},
		}, nil
	}

	out, err := comatproto.ServerCreateSession(ctx, &xrpc.Client{Host: host}, &comatproto.ServerCreateSession_Input{
		Identifier: handle,
		Password:   password,
	})
	if err != nil {
		return nil, err
	}
	s.storeBotSession(out.AccessJwt, out.RefreshJwt, out.Did)
	return &xrpc.Client{
		Host: host,
		Auth: &xrpc.AuthInfo{AccessJwt: out.AccessJwt, RefreshJwt: out.RefreshJwt, Did: out.Did, Handle: handle},
	}, nil
}

// refreshBotSession exchanges a cached refresh token for a fresh
// access/refresh pair. com.atproto.server.refreshSession authenticates via
// the refresh token itself as the bearer, not the (expired) access token —
// hence the one-off client with Auth.AccessJwt set to refreshJwt.
func (s *Service) refreshBotSession(ctx context.Context, refreshJwt string) (*xrpc.Client, error) {
	host := s.botPDSHost()
	refreshClient := &xrpc.Client{Host: host, Auth: &xrpc.AuthInfo{AccessJwt: refreshJwt}}
	out, err := comatproto.ServerRefreshSession(ctx, refreshClient)
	if err != nil {
		return nil, err
	}
	s.storeBotSession(out.AccessJwt, out.RefreshJwt, out.Did)
	return &xrpc.Client{
		Host: host,
		Auth: &xrpc.AuthInfo{AccessJwt: out.AccessJwt, RefreshJwt: out.RefreshJwt, Did: out.Did, Handle: s.cfg.ATProtoBotHandle},
	}, nil
}

func (s *Service) botPDSHost() string {
	var host string
	s.db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'atproto_bot_pds_host'`).Scan(&host)
	if host == "" {
		return defaultBotPDSHost
	}
	return host
}

func (s *Service) cachedBotSession() (accessJwt, refreshJwt, did string) {
	s.db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'atproto_bot_access_jwt'`).Scan(&accessJwt)
	s.db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'atproto_bot_refresh_jwt'`).Scan(&refreshJwt)
	s.db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'atproto_bot_did'`).Scan(&did)
	return
}

func (s *Service) storeBotSession(accessJwt, refreshJwt, did string) {
	s.db.Exec(`INSERT INTO instance_settings (key, value) VALUES ('atproto_bot_access_jwt', $1)
		ON CONFLICT (key) DO UPDATE SET value = $1`, accessJwt)
	s.db.Exec(`INSERT INTO instance_settings (key, value) VALUES ('atproto_bot_refresh_jwt', $1)
		ON CONFLICT (key) DO UPDATE SET value = $1`, refreshJwt)
	s.db.Exec(`INSERT INTO instance_settings (key, value) VALUES ('atproto_bot_did', $1)
		ON CONFLICT (key) DO UPDATE SET value = $1`, did)
}
