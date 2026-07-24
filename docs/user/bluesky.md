# Bluesky

Every Agora account is also a real Bluesky account — no bridge, no separate signup. Agora runs its own copy of the same technology Bluesky itself runs on (the **AT Protocol**), so your public posts sync to and from Bluesky directly, and you can follow any Bluesky account natively, right alongside your fediverse and Agora connections.

> This is a different network from [The Fediverse](fediverse.md) (Mastodon and friends) — they use different underlying protocols, so following works a little differently on each, even though both live on the same **Connections** page.

## What this means for you

- **Your public posts appear on Bluesky.** Anyone on Bluesky can find, follow, and see them — this only applies to posts set to **Public**. Friends-only and private posts never leave Agora.
- **You can follow native Bluesky accounts** — just their handle (`someone.bsky.social`) or DID, no bridge in between.
- **Replies, likes, reposts, and quotes go both ways.** Reply to a Bluesky post from Agora and they see it there; like or repost a Bluesky post and it shows up as a real like/repost on their side too. Quoting a Bluesky post shows the quoted content inline, the same as a local quote-share.
- **Search covers Bluesky too.** The unified **Search** page has an "On Bluesky" section for both People and Posts — this is a live, network-wide search against Bluesky itself, not limited to accounts you already follow.
- **Very long posts get trimmed.** Bluesky caps post length well below Agora's own limit. If a post is too long to fit, what appears on Bluesky is truncated with a link back to the full post on Agora.
- **Content warnings cross over as a generic warning.** Bluesky doesn't have free-text content warnings like Agora does — a CW you set still applies a warning label on the Bluesky side, but the specific wording doesn't carry over, just the fact that a warning exists.

## Turning it off

Bluesky federation is **on by default** but fully optional, with two levels of control in **Settings → Bluesky**:

- **Bluesky (AT Protocol)** — turn this off and your posts stop reaching Bluesky entirely, going forward.
- **Bluesky post notifications** — a separate toggle just for *notifications about accounts you follow*. Turning this off doesn't affect whether your own posts federate.

## Following a Bluesky account

1. Go to **Connections** in the left nav and pick the **Bluesky** tab.
2. Enter their handle (e.g. `user.bsky.social`) or DID.
3. Confirm the preview and click **Follow**.
4. By default their posts don't appear in your main feed — flip the **home icon** on their row to include them, or go to **My Feeds** and build a custom feed around the "specific Bluesky account" or "all followed Bluesky accounts" filter. You can also exclude one specific followed account from a custom feed if you want everyone *except* them.

You can also follow directly from a Bluesky account's profile page once you've seen one of their posts. If they follow you back, you'll see a **"Follows you"** pill next to their name.

## Notifications are per-account, not automatic

Same as the fediverse: following someone on Bluesky doesn't automatically notify you of every new post. Tap the **bell icon** on their Connections → Bluesky row (or on their profile) to turn on notifications for that one account specifically. The account-wide "Bluesky post notifications" toggle in Settings still has to be on too.

## Search

The **Search** page's People and Posts tabs each include an "On Bluesky" section — a genuinely live search against the wider Bluesky network, not just accounts you already follow or posts already visible to you. Results link out to `bsky.app`. Posts tagged with a hashtag are searchable the same way local hashtags are.

## What you can't do with a Bluesky account

Bluesky accounts don't support Agora's friend system — there's no AT Protocol equivalent of a mutual friend request, so a Bluesky profile shows Follow/Unfollow instead of Add Friend. You *can* add a Bluesky account (once it's shown up locally, which happens the first time you see one of their posts) to a **Friend List**, right alongside your Agora friends and fediverse follows. You can also block a Bluesky account the same way you'd block anyone else — see [Blocking someone](blocking.md).

## If you were already following someone through the fediverse bridge

Before native Bluesky support existed, the only way to reach a Bluesky account from Agora was through the [Bridgy Fed](https://fed.brid.gy/) bridge, which makes a Bluesky account show up as a fediverse actor. If you're still following someone that way, their row on the **Connections → Fediverse** tab will offer a **"Migrate to Bluesky"** button — it creates a native Bluesky follow without unfollowing the bridged version, so you don't lose anything in the switch.
