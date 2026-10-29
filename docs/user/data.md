# Managing Your Data

## Downloading your data

You can download a copy of your data as a ZIP file:

- **On the web:** go to **Settings → Data** and click **Export data (ZIP)**.
- **In the mobile app:** go to **Settings**, and under **Data** tap **Export my data**. When the export is ready, your phone's share sheet opens so you can save it to Files or send it somewhere else.

The ZIP contains JSON files:

| File | What's in it |
|---|---|
| `profile.json` | Your username, email address, display name, bio, avatar, location, website, and when you joined |
| `posts.json` | Your posts and comments: the text, image, visibility, and when you wrote them |
| `comments.json` | Your comments, each with the post it was written on |
| `friends.json` | Your friends, and when you became friends |
| `likes.json` | Likes recorded under the older like system |

The export doesn't include your direct messages, your reactions, or your Fediverse and Bluesky follows.

## Resetting your feed history

Your feed's ranking learns from what you interact with. To start fresh, go to **Settings** in the mobile app and, under **Data**, tap **Reset feed history**. Your feed goes back to the default ranking. This doesn't delete any posts.

## Deleting your account

Deleting your account doesn't happen straight away. It's scheduled, with a grace period before anything is removed. The grace period is **30 days** unless your instance's administrator has set a different one.

- **On the web:** go to **Settings → Data**, click **Schedule deletion**, and confirm.
- **In the mobile app:** go to **Settings**, and under **Data** tap **Delete account**, then confirm **Delete account**.

During the grace period, other people can no longer open your profile or find you in search, but you can still sign in.

When the grace period ends, your account and everything in it is permanently deleted from this instance: your profile, posts, comments, photos, friends, and messages. This can't be undone. Copies of public posts that already reached Mastodon, Bluesky or other servers are kept by those servers.

### Cancelling a scheduled deletion

You can cancel any time before the grace period ends:

- **On the web:** go to **Settings → Data**, where **Delete account** shows the scheduled date, and click **Cancel deletion**.
- **In the mobile app:** go to **Settings**, and under **Data** tap **Cancel account deletion**. The confirmation shows the scheduled date. Tap **Cancel deletion**.

Your profile and posts become visible again straight away.

> If you might want your posts later, download your data (see above) before you schedule the deletion.

## Related

- [Privacy settings](privacy.md)
- [Your profile](profile.md)
- [Blocking someone](blocking.md)
