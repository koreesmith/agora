package atproto

import (
	"context"
	"log"
	"time"

	"github.com/bluesky-social/indigo/api/bsky"
)

// AGORA-313: a Bluesky follow reaches us through no channel at all. It is a
// record written into the follower's own repo and delivered nowhere, which is
// why there has never been an at_followers to match ap_followers (see the
// AGORA-249 note on ListFollowing in follow.go). Two ways to learn about one:
// consume the network firehose, or ask the AppView. AGORA-197 already made
// this call for post ingestion (subscribeRepos has no server-side DID filter,
// so consuming it means parsing every commit on the network), and follows are
// a higher-volume record type than posts, so the same reasoning applies with
// more force. Polling getFollowers costs one request per local account per
// interval and scales with this instance's account count instead of the
// network's size.
const followerPollInterval = 15 * time.Minute

// A follow is not time-critical the way a reply is, so the interval above is
// deliberately slacker than authorFeedPollInterval. These two bound the work
// per account per tick: getFollowers caps limit at 100, and an account whose
// follower list is longer than followerMaxPages*followerPageLimit is walked
// only that far. See pollFollowersFor for what truncation costs.
const followerPageLimit = 100
const followerMaxPages = 10

// StartBlueskyFollowerPolling walks each local AT Proto account's follower
// list on an interval and notifies on arrivals. Mirrors
// StartBlueskyIngestion's shape, including the immediate first poll, but runs
// on its own ticker rather than joining that one: it is the slowest of the
// pollers and the only one whose per-tick cost scales with local account
// count rather than with what those accounts follow or post.
func (s *Service) StartBlueskyFollowerPolling(ctx context.Context) {
	ticker := time.NewTicker(followerPollInterval)
	defer ticker.Stop()

	poll := func() {
		type account struct {
			userID string
			did    string
			seeded bool
		}
		rows, err := s.db.QueryContext(ctx, `
			SELECT id, atproto_did, atproto_followers_seeded
			FROM users
			WHERE atproto_did != '' AND is_remote = false
			  AND atproto_enabled = true AND deletion_scheduled_at IS NULL
		`)
		if err != nil {
			return
		}
		var accounts []account
		for rows.Next() {
			var a account
			if rows.Scan(&a.userID, &a.did, &a.seeded) == nil {
				accounts = append(accounts, a)
			}
		}
		rows.Close()

		for _, a := range accounts {
			s.pollFollowersFor(ctx, a.userID, a.did, a.seeded)
		}
	}

	poll()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			poll()
		}
	}
}

// pollFollowersFor diffs one local account's current follower set against
// at_followers, notifying for arrivals and dropping rows for departures.
//
// `seeded` false means this account has never been walked: every follower
// found predates the feature, so the walk fills at_followers silently and only
// later ticks notify. The flag is persisted even when the walk turns up
// nothing, so an account with no followers yet still gets a notification for
// its genuine first one.
//
// A truncated walk (more followers than followerMaxPages*followerPageLimit)
// stops short and skips the removal half of the diff, since the followers it
// never fetched are indistinguishable from ones that went away. Arrivals still
// land: getFollowers returns newest first, so the accounts a truncated walk
// misses are the oldest, which are precisely the ones already recorded.
func (s *Service) pollFollowersFor(ctx context.Context, localUserID, did string, seeded bool) {
	current := map[string]*bsky.ActorDefs_ProfileView{}
	cursor := ""
	truncated := true
	for page := 0; page < followerMaxPages; page++ {
		out, err := bsky.GraphGetFollowers(ctx, s.appviewClient(), did, cursor, followerPageLimit)
		if err != nil {
			return // leave at_followers alone rather than diffing against a partial set
		}
		for _, f := range out.Followers {
			if f != nil {
				current[f.Did] = f
			}
		}
		if out.Cursor == nil || *out.Cursor == "" || len(out.Followers) == 0 {
			truncated = false
			break
		}
		cursor = *out.Cursor
	}

	existing := map[string]bool{}
	if rows, err := s.db.QueryContext(ctx,
		`SELECT follower_did FROM at_followers WHERE local_user_id = $1`, localUserID); err == nil {
		for rows.Next() {
			var followerDID string
			if rows.Scan(&followerDID) == nil {
				existing[followerDID] = true
			}
		}
		rows.Close()
	}

	for followerDID, actor := range current {
		if existing[followerDID] {
			continue
		}
		handle, displayName, avatarURL := actorFields(actor)
		// AGORA-205: same enforcement point every other inbound AT Proto path uses.
		if s.isBlueskyActorBlocked(followerDID, handle) {
			continue
		}
		res, err := s.db.ExecContext(ctx, `
			INSERT INTO at_followers (local_user_id, follower_did) VALUES ($1, $2)
			ON CONFLICT (local_user_id, follower_did) DO NOTHING
		`, localUserID, followerDID)
		if err != nil {
			continue
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue // already recorded, expected on a re-poll rather than an error
		}
		if !seeded {
			continue // silent fill, see the doc comment
		}
		// The stub is created only for a follower we're about to name in a
		// notification, so a seeding pass doesn't manufacture a users row per
		// pre-existing follower.
		followerID, err := s.getOrCreateRemoteATUser(followerDID, handle, displayName, avatarURL, "")
		if err != nil {
			continue
		}
		if s.notif != nil && followerID != localUserID {
			s.notif.Create(localUserID, followerID, "atproto_follow", "", "")
		}
		log.Printf("atproto: new Bluesky follower %s for user %s", handle, localUserID)
	}

	if !truncated {
		for followerDID := range existing {
			if _, ok := current[followerDID]; ok {
				continue
			}
			s.db.ExecContext(ctx,
				`DELETE FROM at_followers WHERE local_user_id = $1 AND follower_did = $2`, localUserID, followerDID)
		}
	}

	if !seeded {
		s.db.ExecContext(ctx,
			`UPDATE users SET atproto_followers_seeded = true WHERE id = $1`, localUserID)
	}

	// AGORA-348: reaching this point means every page fetched above succeeded
	// (an error mid-walk returns early, before this), so the walk is worth
	// recording as a sync even when it turned up no changes at all.
	s.db.ExecContext(ctx,
		`UPDATE users SET atproto_followers_synced_at = NOW() WHERE id = $1`, localUserID)
}
