# The Fediverse

Agora speaks **ActivityPub**, the protocol behind Mastodon and the rest of the "fediverse." This means your public posts and Mastodon's are part of the same conversation — no separate account needed.

> Looking for native Bluesky support instead? Bluesky uses a different protocol (AT Protocol, not ActivityPub) and has its own page — see [Bluesky](bluesky.md). Both fediverse follows and Bluesky follows now live together on one **Connections** page, alongside your Agora friends.

## What this means for you

- **Your public posts are discoverable from Mastodon and similar apps.** Anyone there can find, follow, and see your public posts — this only ever applies to posts set to **Public**. Friends-only and private posts are never federated, full stop.
- **Replies go both ways.** Reply to a Mastodon post from Agora, and they see it on Mastodon. They reply, and it shows up back in Agora. Editing or deleting a federated post or reply updates or removes it on the other side too.
- **Quotes go both ways too.** Mastodon and other apps that support quote posts can quote your Agora posts, not just boost/repost them. On servers that don't support quoting yet, a quote-share still shows up as a link back to the original.
- **You can follow fediverse accounts.** Go to **Connections → Fediverse** in the left nav, enter a handle like `user@mastodon.social`, and follow them — their public posts start showing up in a custom feed you build around that follow.
- **You can @mention fediverse accounts.** Type `@someone@instance.social` in a post or comment and, if that account can be resolved, they'll get a real notification on their own server — whether or not you follow them or are replying to them.
- **Mastodon custom emoji render properly.** A `:shortcode:` in a Mastodon user's display name, bio, or post shows as the actual inline image they intended, not literal text.

## Turning it off

Fediverse federation is **on by default** but fully optional, with two levels of control:

- **Settings → Fediverse → ActivityPub (Fediverse)** — turn this off and your posts stop being visible to the fediverse entirely, going forward.
- **Settings → Fediverse → Fediverse post notifications** — a separate toggle just for *notifications about accounts you follow*. Turning this off doesn't affect whether your own posts federate.

## Notifications are per-account, not automatic

Following a fediverse account doesn't automatically notify you every time they post — same as following someone locally. If you want to be notified about a specific account's new posts, tap the **bell icon** next to them on the Connections → Fediverse follow list, or directly on their profile page. The account-wide "Fediverse post notifications" toggle above still has to be on too — think of it as the master switch, and the per-account bell as the finer control underneath it.

## Following a fediverse account

1. Go to **Connections** in the left nav and pick the **Fediverse** tab.
2. Enter their full handle (e.g. `user@mastodon.social`) or a profile URL — there's no way to search the fediverse by name, the same limitation Mastodon's own remote search has.
3. Confirm the preview and click **Follow**.
4. By default their posts don't appear in your main feed — flip the **home icon** on their row to include them, or go to **My Feeds** and build a custom feed around the "specific fediverse account" or "all followed fediverse accounts" filter.

You can also follow directly from a fediverse account's own profile page — click through from any post of theirs, and you'll see the same Follow/notify controls there. If they follow you back, you'll see a **"Follows you"** pill next to their name.

## If the account turns out to be a bridged Bluesky account

Some Bluesky users are only reachable through the [Bridgy Fed](https://fed.brid.gy/) bridge (a handle ending in `.ap.brid.gy` or similar). If you search for one on the Fediverse tab, Agora will point this out and offer to take you to the **Bluesky tab** instead, so you follow them natively rather than through the bridge. If you're already following one this way, look for a **"Migrate to Bluesky"** button on their row — it switches you to a native follow without unfollowing the bridged account.

## What you can't do with a fediverse account

Fediverse accounts don't support Agora's friend system — there's no ActivityPub equivalent of a friend request, so a fediverse profile shows Follow/Unfollow instead of Add Friend. You *can* still add a fediverse account (once they've shown up locally, which happens the first time you see one of their posts) to a **Friend List**, right alongside your Agora friends and Bluesky follows. You can also block a fediverse account the same way you'd block anyone else — see [Blocking someone](blocking.md).

## Content warnings

A content warning you set on a post is sent along as the fediverse's own content-warning field — Mastodon and similar apps show your post behind a "show more" prompt using whatever warning text you wrote, the same as they'd show one of their own users' CWs.
