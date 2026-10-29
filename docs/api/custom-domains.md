# Custom Domains API

The claim/verify/approve workflow behind "bring your own domain" (AGORA-278) — a user proving they control a domain so it can stand in for their instance-issued handle. See [AT Protocol API](atproto.md) for what a verified domain actually does once it's live, and [Using your own domain as your handle](../user/custom-domain.md) for the user-facing walkthrough.

Implemented by `internal/domains`, which owns the workflow and knows nothing about what a verified domain is *for*. `internal/atproto` reads the `custom_domains` table directly for identity purposes and never touches the workflow.

## Concepts

**Two independent status axes.** `verification_status` (`pending` / `verified` / `failed`) is whether DNS still proves the user controls the domain. `approval_status` (`pending` / `approved` / `rejected`) is whether the instance has agreed to publish it. Either can change without the other, and a handle is live only when both say yes — the `live` boolean in every response is that conjunction, computed server-side so no client re-derives it.

**One claim per account.** An AT Protocol DID has exactly one handle, so the endpoints are singular and take no id. Claiming replaces any existing row.

**Protocol scoping.** Every row carries a `protocol` (`atproto` today). The table is intended to be shared with the future ActivityPub custom-domain work, which verifies ownership identically.

---

## User endpoints

All require authentication. `POST /custom-domain` is limited to 10/hour and `POST /custom-domain/verify` to 20/hour **per account** — both make the instance perform outbound DNS and HTTPS lookups against a name the caller chose.

### `GET /api/custom-domain`

Everything the settings panel needs, including generated setup instructions.

**Response 200:**
```json
{
  "approval_mode": "manual",
  "available": true,
  "did": "did:web:alice.agora.example.com",
  "fallback_handle": "alice.agora.example.com",
  "current_handle": "alice.example",
  "claim": {
    "id": "…",
    "domain": "alice.example",
    "verification_method": "dns",
    "verification_status": "verified",
    "approval_status": "approved",
    "live": true,
    "last_error": "",
    "rejection_reason": "",
    "verified_at": "…", "last_checked_at": "…", "created_at": "…"
  },
  "instructions": {
    "dns_record_type": "TXT",
    "dns_record_name": "_atproto.alice.example",
    "dns_record_value": "did=did:web:alice.agora.example.com",
    "well_known_url": "https://alice.example/.well-known/atproto-did",
    "well_known_content": "did:web:alice.agora.example.com"
  }
}
```

`claim` and `instructions` are absent when nothing is claimed. `current_handle` is the custom domain when live and the instance handle otherwise.

`available: false` (with `unavailable_reason`) means AT Proto is off instance-wide or for this account, which makes a custom handle meaningless rather than merely unavailable.

Calling this mints and persists the user's DID if nothing has needed it yet — that's what lets the panel show a copy-pasteable record before the user has done anything.

### `POST /api/custom-domain`

**Body:** `{"domain": "alice.example"}` — liberally normalized (a pasted URL, a leading `@`, mixed case, a port, a trailing dot are all accepted).

**Response 201:** `{"claim": {…}, "did": "…", "instructions": {…}}`

Does not verify anything: the user hasn't created the record yet at this point, so an immediate check would fail by construction and read as an error rather than the next step.

**422** — not a usable domain. Notably rejects IP literals and anything inside the instance's own domain: that namespace holds the auto-generated per-user handles and its DNS is controlled by the operator rather than the claimant, which is the one impersonation risk domain verification cannot rule out.

**409** — AT Proto is disabled, or another account holds the domain. A *verified* claim holds it indefinitely; an unverified one is released after 7 days, since an unverified claim requires no proof and would otherwise be a free permanent block on the real owner.

### `POST /api/custom-domain/verify`

Runs the challenge (DNS TXT at `_atproto.<domain>`, else an HTTPS well-known file) and records the verdict.

**Response 200:** `{"claim": {…}, "instructions": {…}}` — on failure, `last_error` explains what was actually found.

Side effects on a status change: auto-approval if the instance is in `auto` mode, a `#identity` firehose event when the handle starts or stops being live, and a notification.

### `DELETE /api/custom-domain`

Drops the claim, reverting the user to their instance handle.

---

## Admin endpoints

Require an admin role.

### `GET /api/admin/custom-domains`

**Query:** `status=pending` (default — verified but unapproved, i.e. what needs a decision) or `status=all`.

**Response 200:** `{"domains": [{…claim fields…, "user_id", "username", "display_name", "did"}], "approval_mode": "manual"}`

### `POST /api/admin/custom-domains/{id}/approve`

**422** if the claim isn't currently verified — approving one would publish a handle the domain's DNS doesn't back.

### `POST /api/admin/custom-domains/{id}/reject`

**Body:** `{"reason": "…"}` (optional, shown to the user, truncated at 500 chars).

Also used to revoke an already-approved domain, which takes the handle back out of service.

Both actions write to the audit log and notify the user without naming the reviewing admin.

---

## Approval mode

`instance_settings['custom_domain_approval']` is `manual` (default) or `auto`, exposed through the normal admin settings endpoints and the Domains tab. Anything other than an explicit `auto` is treated as manual.

---

## Background re-verification

`StartReverification` re-runs the challenge for claims that have verified at least once, every 12 hours. This is what makes a removed DNS record or a lapsed registration take the handle out of service on its own: nothing tears it down, the claim simply stops satisfying the both-axes query every identity surface asks. A claim that has *never* verified is never re-checked — the user hasn't created the record yet, and repeatedly resolving names on their behalf is exactly the load the claim rate limit exists to prevent.
