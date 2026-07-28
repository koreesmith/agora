package atproto

import (
	"context"
	"log"
	"net/http"
	"time"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/xrpc"
)

// defaultRelayHost is the Bluesky relay Agora asks to crawl its firehose,
// absent an admin override. AGORA-193 will add the actual settings UI for
// this; until then it reads the same instance_settings key/value row that
// UI would write to (federation.go's federation_enabled/activitypub_enabled
// read the same way), so no migration is needed once that ticket lands.
const defaultRelayHost = "bsky.network"

var relayHTTPClient = &http.Client{Timeout: 10 * time.Second}

func (s *Service) relayHost() string {
	var host string
	s.db.QueryRow(`SELECT value FROM instance_settings WHERE key = 'atproto_relay_host'`).Scan(&host)
	if host == "" {
		return defaultRelayHost
	}
	return host
}

// requestCrawl sends com.atproto.sync.requestCrawl to the configured relay —
// this is a one-shot "come crawl me" nudge, not a subscription itself; the
// relay reacts by opening its own connection to our subscribeRepos firehose
// (AGORA-191), same direction the AC describes.
func (s *Service) requestCrawl(ctx context.Context) error {
	c := &xrpc.Client{
		Client: relayHTTPClient,
		Host:   "https://" + s.relayHost(),
	}
	return comatproto.SyncRequestCrawl(ctx, c, &comatproto.SyncRequestCrawl_Input{
		Hostname: domainFromURL(s.cfg.InstanceDomain),
	})
}

// StartRelayCrawl requests a relay crawl on startup, retrying with
// exponential backoff on failure (mirroring drainQueue's 2^attempts-minutes,
// capped-at-24h shape in internal/federation/federation.go), and keeps
// re-requesting periodically after success too. requestCrawl has no
// subscription state of its own to inspect, so there's no way to detect a
// relay silently dropping its firehose connection — reconfirming on an
// interval is the only available recovery, cheap since the call is
// idempotent from the relay's side.
func (s *Service) StartRelayCrawl(ctx context.Context) {
	const maxBackoff = 24 * time.Hour
	const reconfirmInterval = 6 * time.Hour
	const disabledPollInterval = 5 * time.Minute
	backoff := time.Minute

	for {
		// AGORA-193: re-checked every cycle rather than once at startup —
		// same anti-pattern federation.StartBackgroundSync's own comment
		// warns against — so toggling the instance-wide flag off actually
		// stops crawl requests, and back on resumes them, without a restart.
		if !s.atprotoEnabled() {
			select {
			case <-ctx.Done():
				return
			case <-time.After(disabledPollInterval):
			}
			continue
		}

		wait := reconfirmInterval
		if err := s.requestCrawl(ctx); err != nil {
			log.Printf("atproto: requestCrawl to relay %s failed: %v", s.relayHost(), err)
			wait = backoff
			if backoff *= 2; backoff > maxBackoff {
				backoff = maxBackoff
			}
		} else {
			log.Printf("atproto: requested crawl from relay %s", s.relayHost())
			backoff = time.Minute
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}
