# The Fediverse

Agora speaks **ActivityPub**, the protocol behind Mastodon and the rest of the "fediverse." This means your public posts and Mastodon's are part of the same conversation — no separate account needed.

## What this means for you

- **Your public posts are discoverable from Mastodon and similar apps.** Anyone there can find, follow, and see your public posts — this only ever applies to posts set to **Public**. Friends-only and private posts are never federated, full stop.
- **Replies go both ways.** Reply to a Mastodon post from Agora, and they see it on Mastodon. They reply, and it shows up back in Agora. Editing or deleting a federated post or reply updates or removes it on the other side too.
- **You can follow fediverse accounts.** Go to **Fediverse** in the left nav, enter a handle like `user@mastodon.social`, and follow them — their public posts start showing up in a custom feed you build around that follow.
- **You can @mention fediverse accounts.** Type `@someone@instance.social` in a post or comment and, if that account can be resolved, they'll get a real notification on their own server — whether or not you follow them or are replying to them.

## Turning it off

Fediverse federation is **on by default** but fully optional, with two levels of control:

- **Settings → Privacy → Fediverse (ActivityPub)** — turn this off and your posts stop being visible to the fediverse entirely, going forward. (Same toggle also appears on the Fediverse page itself, for convenience.)
- **Settings → Notifications → Fediverse notifications** — a separate toggle just for *notifications about accounts you follow*. Turning this off doesn't affect whether your own posts federate.

## Notifications are per-account, not automatic

Following a fediverse account doesn't automatically notify you every time they post — same as following someone locally. If you want to be notified about a specific account's new posts, tap the **bell icon** next to them, either on the Fediverse page's follow list or directly on their profile page. The account-wide "Fediverse notifications" toggle above still has to be on too — think of it as the master switch, and the per-account bell as the finer control underneath it.

## Following a fediverse account

1. Go to **Fediverse** in the left nav.
2. Enter their full handle (e.g. `user@mastodon.social`) or a profile URL — there's no way to search the fediverse by name, the same limitation Mastodon's own remote search has.
3. Confirm the preview and click **Follow**.
4. Their posts appear once you build a custom feed around them — go to **My Feeds**, create a feed, and choose the "specific fediverse account" or "all followed fediverse accounts" filter.

You can also follow directly from a fediverse account's own profile page — click through from any post of theirs, and you'll see the same Follow/notify controls there.

## What you can't do with a fediverse account

Fediverse accounts don't support Agora's friend system — there's no ActivityPub equivalent of a friend request, so a fediverse profile shows Follow/Unfollow instead of Add Friend. You *can* still block a fediverse account the same way you'd block anyone else; blocking is a local decision and works regardless of where the other account lives.

## Content warnings

A content warning you set on a post is sent along as the fediverse's own content-warning field — Mastodon and similar apps show your post behind a "show more" prompt using whatever warning text you wrote, the same as they'd show one of their own users' CWs.
