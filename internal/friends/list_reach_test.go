package friends

import (
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/store"
	"github.com/lib/pq"
)

// AGORA-345: the composer stays quiet unless a limited audience genuinely
// contains somebody whose server cannot honour the limit, so the classification
// behind that decision has to be exact in both directions. Counting a remote
// Agora friend as lossy would put a warning on posts that work perfectly, which
// trains people to ignore it; missing a Bluesky member means somebody is
// silently not delivered to.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips if
// it isn't reachable rather than failing the suite.
func TestListGroupsClassification(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil { t.Skipf("skipping: agora-postgres-test not reachable: %v", err) }
	defer db.Close()
	if err := db.Migrate(); err != nil { t.Fatalf("migrate: %v", err) }
	u := time.Now().UnixNano()

	var owner string
	db.QueryRow(`INSERT INTO users (username,email,password_hash) VALUES ($1,$1,'x') RETURNING id`, fmt.Sprintf("lg_o_%d", u)).Scan(&owner)
	defer db.Exec(`DELETE FROM users WHERE id=$1`, owner)

	agoraPeer := fmt.Sprintf("agorapeer%d.example", u)
	db.Exec(`INSERT INTO federated_instances (domain,name,instance_url,status) VALUES ($1,'Peer',$2,'active')`, agoraPeer, "https://"+agoraPeer)
	defer db.Exec(`DELETE FROM federated_instances WHERE domain=$1`, agoraPeer)

	mk := func(tag, inst, actor, legacy, display string) string {
		var id string
		db.QueryRow(`INSERT INTO users (username,email,password_hash,display_name,is_remote,remote_instance,ap_actor_url,remote_user_id)
			VALUES ($1,$1,'x',$2,$3,$4,$5,$6) RETURNING id`,
			fmt.Sprintf("lg_%s_%d", tag, u), display, inst != "", inst, actor, legacy).Scan(&id)
		return id
	}
	local := mk("loc", "", "", "", "Local Pal")
	db.Exec(`UPDATE users SET is_remote=false WHERE id=$1`, local)
	remoteAgora := mk("ag", agoraPeer, "https://"+agoraPeer+"/federation/users/bob", "", "Remote Agora")
	legacyStub  := mk("lg", "someold.example", "", "carol", "Legacy Stub")
	masto       := mk("md", "mastodon.social", "https://mastodon.social/users/dave", "", "Mastodon Dave")
	bsky        := mk("bs", "bsky.app", "https://bsky.app/x", "", "Bluesky Erin")
	for _, id := range []string{local, remoteAgora, legacyStub, masto, bsky} {
		defer db.Exec(`DELETE FROM users WHERE id=$1`, id)
	}

	var gid string
	db.QueryRow(`INSERT INTO friend_groups (user_id,name) VALUES ($1,$2) RETURNING id`, owner, fmt.Sprintf("Mixed %d", u)).Scan(&gid)
	defer db.Exec(`DELETE FROM friend_groups WHERE id=$1`, gid)
	for _, id := range []string{local, remoteAgora, legacyStub, masto, bsky} {
		if _, err := db.Exec(`INSERT INTO friend_group_members (group_id,friend_id) VALUES ($1,$2)`, gid, id); err != nil {
			t.Fatalf("seed member: %v", err)
		}
	}

	var bc, fc int
	var bn, fn []string
	err = db.QueryRow(`
		SELECT COUNT(*) FILTER (WHERE `+isBluesky+`),
		       COALESCE((array_agg(`+memberLabel+`) FILTER (WHERE `+isBluesky+`))[1:3],'{}'),
		       COUNT(*) FILTER (WHERE `+isFediverse+`),
		       COALESCE((array_agg(`+memberLabel+`) FILTER (WHERE `+isFediverse+`))[1:3],'{}')
		FROM friend_group_members m JOIN users u ON u.id=m.friend_id WHERE m.group_id=$1
	`, gid).Scan(&bc, pq.Array(&bn), &fc, pq.Array(&fn))
	if err != nil { t.Fatalf("query: %v", err) }

	if bc != 1 || len(bn) != 1 || bn[0] != "Bluesky Erin" {
		t.Errorf("bluesky: count=%d names=%v, want 1 / [Bluesky Erin]", bc, bn)
	}
	if fc != 1 || len(fn) != 1 || fn[0] != "Mastodon Dave" {
		t.Errorf("fediverse: count=%d names=%v, want 1 / [Mastodon Dave]. A remote Agora member or a legacy stub must not be counted lossy.", fc, fn)
	}
}
