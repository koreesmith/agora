package users

import (
	"database/sql"
	"net/http"

	"github.com/agora-social/agora/internal/auth"
)

// AGORA-387: backend for the getting started wizard (AGORA-386). Whether an
// account still needs the wizard is users.onboarding_completed_at, reported
// to the client as onboarding_completed by login and /auth/me.

// CompleteOnboarding marks the caller as done with the wizard, whether they
// finished it or skipped it. Idempotent: a replay from the Help menu that
// reaches the end again keeps the original timestamp.
func (s *Service) CompleteOnboarding(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	if userID == "" {
		writeError(w, 401, "unauthorized")
		return
	}
	if _, err := s.db.Exec(`
		UPDATE users SET onboarding_completed_at = COALESCE(onboarding_completed_at, NOW())
		WHERE id = $1
	`, userID); err != nil {
		writeError(w, 500, "update failed")
		return
	}
	writeJSON(w, 200, map[string]bool{"onboarding_completed": true})
}

type onboardingPerson struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	DisplayName  string `json:"display_name"`
	AvatarURL    string `json:"avatar_url"`
	Bio          string `json:"bio"`
	FriendStatus string `json:"friend_status"`
	// Reason is why this person is suggested, when there is a specific one.
	Reason string `json:"reason,omitempty"`
}

func firstReason(reasons []string) string {
	if len(reasons) == 0 {
		return ""
	}
	return reasons[0]
}

// onboardingFriendStatus is the caller's ($1) relationship with u, in the
// same vocabulary Discover uses for pending requests, plus 'friends'.
const onboardingFriendStatus = `COALESCE((
	SELECT CASE
		WHEN fs.status = 'accepted' THEN 'friends'
		WHEN fs.requester_id = $1 THEN 'pending'
		ELSE 'pending_incoming'
	END
	FROM friendships fs
	WHERE ((fs.requester_id = $1 AND fs.addressee_id = u.id)
	    OR (fs.addressee_id = $1 AND fs.requester_id = u.id))
	  AND fs.status IN ('pending', 'accepted')
	LIMIT 1
), '')`

// onboardingNotBlocked drops anyone the caller ($1) has blocked or been
// blocked by.
const onboardingNotBlocked = `NOT EXISTS (
	SELECT 1 FROM blocks b
	WHERE (b.blocker_id = $1 AND b.blocked_id = u.id)
	   OR (b.blocker_id = u.id AND b.blocked_id = $1)
)`

// OnboardingSuggestions feeds the wizard's Find people step: the account
// whose invite code the caller registered with, then the best matches among
// this instance's accounts (the same ranking Discover uses, which falls back
// to the newest accounts for someone with no connections yet).
//
// The inviter comes from invite_codes, so only invite-mode registrations have
// one. An email invite (/invites/send) only mails a link to /register and
// records nothing about who sent it.
//
// The suggestions leave out private profiles: a brand-new account has no
// reason to know they exist, and their owners chose not to be browsed. The
// inviter is shown whatever their privacy, since they reached out first.
func (s *Service) OnboardingSuggestions(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromCtx(r.Context())
	if userID == "" {
		writeError(w, 401, "unauthorized")
		return
	}

	var inviter *onboardingPerson
	var p onboardingPerson
	err := s.db.QueryRow(`
		SELECT u.id, u.username, u.display_name, u.avatar_url, COALESCE(u.bio, ''),
		       `+onboardingFriendStatus+`
		FROM invite_codes ic
		JOIN users u ON u.id = ic.created_by
		WHERE ic.used_by = $1 AND u.id != $1
		  AND u.is_remote = false AND u.deletion_scheduled_at IS NULL AND u.is_suspended = false
		  AND `+onboardingNotBlocked+`
		ORDER BY ic.used_at DESC NULLS LAST
		LIMIT 1
	`, userID).Scan(&p.ID, &p.Username, &p.DisplayName, &p.AvatarURL, &p.Bio, &p.FriendStatus)
	switch {
	case err == nil:
		inviter = &p
	case err != sql.ErrNoRows:
		writeError(w, 500, "db error")
		return
	}

	// '' never matches a UUID, so with no inviter this excludes nobody.
	inviterID := ""
	if inviter != nil {
		inviterID = inviter.ID
	}

	ranked, err := s.rankedLocalSuggestions(userID, inviterID, 5)
	if err != nil {
		writeError(w, 500, "db error")
		return
	}
	people := make([]onboardingPerson, 0, len(ranked))
	for _, u := range ranked {
		people = append(people, onboardingPerson{
			ID: u.ID, Username: u.Username, DisplayName: u.DisplayName,
			AvatarURL: u.AvatarURL, Bio: u.Bio, FriendStatus: u.FriendStatus,
			Reason: firstReason(u.Reasons),
		})
	}

	writeJSON(w, 200, map[string]any{"inviter": inviter, "people": people})
}
