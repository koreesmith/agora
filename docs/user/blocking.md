# Blocking Someone

## How to block a user

1. Go to the person's **profile page**.
2. Click the **⋯ menu** (three dots) in the top-right corner of their profile.
3. Select **Block @username**.
4. Confirm when prompted.

## What blocking does

When you block someone:
- They cannot see your posts, profile, or activity.
- You will not see their posts in your feed.
- Any existing direct message thread between you is hidden.
- They are silently removed from friend requests.

## Unblocking

1. Go to **Settings → Privacy → Blocked users**.
2. Find the person and click **Unblock**.

---

> **Tip:** Blocking is private — the blocked person is not notified.

## Blocking a fediverse or Bluesky account

Works the same way, from that account's profile page. If a Mastodon (or other fediverse) user blocks *you*, Agora stops sending them your posts, likes, and replies automatically — you don't need to do anything on your end.

Bluesky works a little differently: because Bluesky has no equivalent of ActivityPub's "notify me when you're blocked," Agora can't automatically detect a Bluesky user blocking you the same way. Blocking a Bluesky account from your own side still works exactly as above and stops their content from reaching you.

## Instance- and account-wide blocks (admin-level)

The block described above is personal — it only affects what *you* see. Instance admins can additionally block an entire fediverse server by domain, or a specific Bluesky account by its DID, instance-wide for everyone on this Agora instance. That's a separate, admin-only control — see your instance's **About** page or ask an admin if you think a whole server or account should be blocked for everyone, not just you.
