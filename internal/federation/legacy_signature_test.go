package federation

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/agora-social/agora/internal/store"
)

// AGORA-316: the legacy Agora-to-Agora protocol shipped with no test coverage
// at all, and the defect that hid there was that the signer and the verifier
// each built the signed byte string their own way. Signing and verifying
// therefore have to be tested against each other, not separately: a test
// that only exercises one side would have passed throughout.

// TestCanonicalActivityIsIndependentOfKeyOrder pins the property the fix rests
// on. The same document is written three ways: Go's alphabetical map ordering,
// Postgres's JSONB (length-then-bytewise) ordering, and an arbitrary
// hand-written order. All three must canonicalize to one byte string.
//
// The JSONB case is the one that mattered in production: federation_queue's
// payload column is JSONB, so what drainQueue reads back is never what
// SendToUserInstance wrote.
func TestCanonicalActivityIsIndependentOfKeyOrder(t *testing.T) {
	orderings := map[string]string{
		"go map order (alphabetical)": `{"actor":"alice","instance_id":"a.example.com","object":{"from_handle":"alice","to_handle":"bob"},"timestamp":1786070397,"type":"friend_request"}`,
		"jsonb order (length, then bytewise)": `{"type": "friend_request", "actor": "alice", "object": {"to_handle": "bob", "from_handle": "alice"}, "timestamp": 1786070397, "instance_id": "a.example.com"}`,
		"arbitrary order": `{"object":{"to_handle":"bob","from_handle":"alice"},"timestamp":1786070397,"instance_id":"a.example.com","type":"friend_request","actor":"alice"}`,
	}

	var want []byte
	var wantName string
	for name, raw := range orderings {
		got, err := canonicalActivity([]byte(raw))
		if err != nil {
			t.Fatalf("%s: canonicalActivity: %v", name, err)
		}
		if want == nil {
			want, wantName = got, name
			continue
		}
		if string(got) != string(want) {
			t.Errorf("canonical form differs by input key order, so a signature can never verify\n  %s: %s\n  %s: %s",
				wantName, want, name, got)
		}
	}
}

// TestCanonicalActivityDropsOnlyTheSignature guards the one field the
// canonical form is allowed to differ by. If it ever dropped or kept anything
// else, a tampered body could canonicalize to the signed bytes.
func TestCanonicalActivityDropsOnlyTheSignature(t *testing.T) {
	raw := []byte(`{"type":"post","actor":"alice","signature":"AAAA","extra":"kept"}`)

	canonical, err := canonicalActivity(raw)
	if err != nil {
		t.Fatalf("canonicalActivity: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(canonical, &m); err != nil {
		t.Fatalf("canonical form is not valid JSON: %v", err)
	}
	if _, present := m["signature"]; present {
		t.Error("signature survived canonicalization, so it would be signing itself")
	}
	for _, k := range []string{"type", "actor", "extra"} {
		if _, present := m[k]; !present {
			t.Errorf("canonicalization dropped %q; only the signature may be removed", k)
		}
	}
}

// TestSignActivityVerifies is the round trip in one process: what signActivity
// puts on the wire must satisfy the same check verifyActivity performs.
func TestSignActivityVerifies(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	// Deliberately in JSONB's key order, since that is the form drainQueue
	// actually reads out of the queue table.
	raw := []byte(`{"type": "friend_request", "actor": "alice", "object": {"to_handle": "bob", "from_handle": "alice"}, "timestamp": 1786070397, "instance_id": "a.example.com"}`)

	wire, err := signActivity(priv, raw)
	if err != nil {
		t.Fatalf("signActivity: %v", err)
	}

	var a Activity
	if err := json.Unmarshal(wire, &a); err != nil {
		t.Fatalf("wire body does not parse as an Activity: %v", err)
	}
	if a.Signature == "" {
		t.Fatal("wire body carries no signature")
	}

	unsigned, err := canonicalActivity(wire)
	if err != nil {
		t.Fatalf("canonicalActivity on the wire body: %v", err)
	}
	sig, err := base64.StdEncoding.DecodeString(a.Signature)
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	if !ed25519.Verify(pub, unsigned, sig) {
		t.Fatal("signature does not verify against the body it was sent with")
	}
}

// TestSignActivityPreservesUndeclaredFields covers the reason the wire body is
// rebuilt from the canonical bytes rather than from the Activity struct. The
// struct declares six fields; anything else in the payload used to be dropped
// on marshal, producing a body that no longer canonicalized to what was
// signed.
func TestSignActivityPreservesUndeclaredFields(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)

	raw := []byte(`{"type":"post","actor":"alice","instance_id":"a.example.com","timestamp":1786070397,"undeclared_field":"must survive"}`)

	wire, err := signActivity(priv, raw)
	if err != nil {
		t.Fatalf("signActivity: %v", err)
	}

	var m map[string]any
	json.Unmarshal(wire, &m)
	if m["undeclared_field"] != "must survive" {
		t.Errorf("a field the Activity struct doesn't declare was dropped from the wire body: %s", wire)
	}
}

// TestVerifyActivityEndToEnd drives the real Inbox verification path against a
// real federated_instances row, which is where the public key comes from.
//
// Requires the local agora-postgres-test instance (localhost:15433); skips if
// it isn't reachable rather than failing the suite.
func TestVerifyActivityEndToEnd(t *testing.T) {
	db, err := store.Open("postgres://agora:agora@localhost:15433/agora_test?sslmode=disable")
	if err != nil {
		t.Skipf("skipping: agora-postgres-test not reachable: %v", err)
	}
	// Registered rather than deferred, and registered first so it runs last:
	// a deferred Close fires before any t.Cleanup, stranding the rows below in
	// the shared test database.
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	s := &Service{db: db}

	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	domain := fmt.Sprintf("agora316-%d.example", time.Now().UnixNano())

	if _, err := db.Exec(`
		INSERT INTO federated_instances (domain, name, public_key, instance_url, status)
		VALUES ($1, 'Test Peer', $2, $3, 'active')
	`, domain, base64.StdEncoding.EncodeToString(pub), "https://"+domain); err != nil {
		t.Fatalf("seed federated_instances: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM federated_instances WHERE domain = $1`, domain) })

	// Insert straight into the queue rather than going through
	// SendToUserInstance, which needs a cfg this Service doesn't have. What
	// matters is that the payload takes the same JSONB round trip a live
	// delivery does: signing bytes that skipped the queue table is precisely
	// the thing that used to look fine in isolation.
	payload := fmt.Sprintf(
		`{"type":"friend_request","actor":"alice","instance_id":%q,"timestamp":1786070397,"object":{"from_handle":"alice","to_handle":"bob"}}`,
		domain)

	var queueID string
	if err := db.QueryRow(`
		INSERT INTO federation_queue (instance_url, payload, next_attempt)
		VALUES ($1, $2, NOW()) RETURNING id
	`, "https://"+domain, payload).Scan(&queueID); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM federation_queue WHERE id = $1`, queueID) })

	var stored []byte
	if err := db.QueryRow(`SELECT payload FROM federation_queue WHERE id = $1`, queueID).Scan(&stored); err != nil {
		t.Fatalf("read back payload: %v", err)
	}
	if string(stored) == payload {
		t.Log("note: JSONB did not reorder this payload; the regression it caused is still covered by the key-order test above")
	}

	wire, err := signActivity(priv, stored)
	if err != nil {
		t.Fatalf("signActivity: %v", err)
	}

	var a Activity
	if err := json.Unmarshal(wire, &a); err != nil {
		t.Fatalf("unmarshal wire body: %v", err)
	}
	if err := s.verifyActivity(wire, a); err != nil {
		t.Fatalf("a validly signed activity was rejected: %v", err)
	}

	t.Run("tampered body is rejected", func(t *testing.T) {
		var m map[string]any
		json.Unmarshal(wire, &m)
		m["object"] = map[string]string{"from_handle": "mallory", "to_handle": "bob"}
		tampered, _ := json.Marshal(m)

		var ta Activity
		json.Unmarshal(tampered, &ta)
		if err := s.verifyActivity(tampered, ta); err == nil {
			t.Fatal("a modified activity passed verification")
		}
	})

	t.Run("signature from the wrong key is rejected", func(t *testing.T) {
		_, otherPriv, _ := ed25519.GenerateKey(rand.Reader)
		forged, err := signActivity(otherPriv, stored)
		if err != nil {
			t.Fatalf("signActivity: %v", err)
		}
		var fa Activity
		json.Unmarshal(forged, &fa)
		if err := s.verifyActivity(forged, fa); err == nil {
			t.Fatal("an activity signed with an unrelated key passed verification")
		}
	})

	t.Run("missing signature is rejected", func(t *testing.T) {
		var m map[string]any
		json.Unmarshal(wire, &m)
		delete(m, "signature")
		unsigned, _ := json.Marshal(m)

		var ua Activity
		json.Unmarshal(unsigned, &ua)
		if err := s.verifyActivity(unsigned, ua); err == nil {
			t.Fatal("an unsigned activity passed verification")
		}
	})
}
