# Using your own domain as your handle

If you own a domain, you can use it as your handle on [Bluesky](bluesky.md) and the wider AT Protocol network — so people find you as **@your-domain.example** instead of the handle this instance issued you.

This is the same mechanism Bluesky itself offers. If you've already set up a custom domain on a Bluesky account, the process here will look familiar.

## What it changes, and what it doesn't

- **It changes the name people see and search for on Bluesky.** Your handle becomes your domain.
- **It doesn't move your account anywhere.** Your posts, friends, followers, photos, and history are untouched, and your underlying identity doesn't change — the custom domain is added as a verified alias, not a migration.
- **Your original handle keeps working.** Anyone who knew you by the handle this instance gave you can still find you by it.
- **It doesn't change your @username here on Agora.** Mentions, your profile link, and how people tag you are all unchanged.
- **You don't have to move your website.** Nothing about your domain's existing site, email, or hosting is affected. You're adding one small record to it, not pointing it here.

## What you need

- A domain you control — one where you can either edit DNS records or upload a file to the web server.
- Bluesky turned on for your account (**Settings → Bluesky**). A custom handle only means something on a network you're actually on.

A domain costs money to register and has to stay registered. If it lapses, so does your handle — see [If your domain stops working](#if-your-domain-stops-working) below.

## Setting it up

### 1. Claim the domain

Go to **Settings → Bluesky** and find the **Custom domain** section. Enter your domain and press **Continue**.

Enter the domain itself (`example.com`), not a full web address. A subdomain works too, if you'd rather be `me.example.com` than `example.com`.

Nothing has changed yet at this point — you've just told us which domain you intend to prove you own.

### 2. Prove you own it

The panel now shows you two options. **You only need one of them.**

**Option 1 — a DNS record.** This is the usual choice, and it works even if your domain has no website at all. At your DNS provider (wherever you bought the domain, or wherever you manage its DNS), add a new record:

| Field | Value |
| --- | --- |
| Type | `TXT` |
| Name | `_atproto.your-domain.example` |
| Value | `did=did:web:...` (copy the exact value from the panel) |

Some providers automatically append your domain to whatever you type in the Name field. If yours does, enter just `_atproto` instead of the full name — otherwise you'll end up with `_atproto.your-domain.example.your-domain.example`, which won't match.

**Option 2 — a file on your website.** If you can upload files to your site but can't edit DNS, put a file at:

```
https://your-domain.example/.well-known/atproto-did
```

It must contain the DID shown in the panel and nothing else, be served over HTTPS, and not redirect anywhere. A redirect is rejected on purpose: the whole point is that *this domain* vouches for you, and a redirect would let some other host answer on its behalf.

### 3. Check verification

Press **Check verification**. If the record is in place, you'll see it confirmed straight away.

If it fails, the panel tells you exactly what it found — a missing record, a value that doesn't match, an unreachable site. DNS changes are not instant: they usually take a few minutes, occasionally up to an hour, to spread. If you're confident the record is right, wait a bit and check again.

### 4. Wait for approval, if your instance asks for one

Some instances publish a verified domain immediately. Others have an administrator review each request first — in which case the panel will say your domain is verified and waiting, and you'll get a notification when it's decided either way.

Verification and approval are two different things. Verification proves you control the domain; approval is the instance deciding whether to publish that handle. A verified domain that's still waiting is completely normal and doesn't mean anything is wrong.

Once it's live, the panel says so, your profile shows your domain next to your @username, and your handle on Bluesky is your domain.

## Keeping it

We re-check your domain periodically, so leave the DNS record or the file in place. Removing it later removes your handle.

## If your domain stops working

If the record disappears, or your domain's registration lapses, verification fails on the next check and your handle **automatically reverts to the one this instance issued you**. Your account is never left in a broken state, and nothing else about it is affected — you keep your posts, your friends, your followers, and your @username.

You'll get a notification when this happens. To get the handle back, put the record back and press **Check verification** again.

## Changing or removing your domain

**Settings → Bluesky → Custom domain → Remove domain** drops the claim and puts you back on your instance-issued handle right away. To switch to a different domain, remove the current one and claim the new one.

You can hold one custom domain at a time: an AT Protocol account has exactly one handle, so a second claim would only raise the question of which one it is.

## If someone else has claimed your domain

A domain can only be claimed by one account at a time, and a claim that has actually been verified holds it for as long as it stays verified.

Someone can, in principle, claim a domain they don't own — but they can't verify it without controlling your DNS, and an unverified claim is released after about a week so it can't be used to sit on your domain indefinitely. If you hit this, wait and try again, or ask your instance's administrator.

## Troubleshooting

**"No TXT records found."** The record hasn't propagated yet, or it's at the wrong name. Check for a doubled domain (`_atproto.example.com.example.com`) — see the note in step 2.

**"Found TXT records, but none matched."** The record exists but its value is wrong. It must be the whole string starting with `did=`, exactly as shown in the panel. Copy it with the Copy button rather than retyping it.

**"The file contains a different DID."** You're serving a file from an older setup, or from a different account. Replace its contents with the value shown in your panel.

**"Could not reach..."** Your site isn't answering over HTTPS. If you don't have a working HTTPS site, use the DNS record instead — it doesn't need one.

**It verified, then stopped.** Something removed the record, or the domain's registration lapsed. See [If your domain stops working](#if-your-domain-stops-working).

**Bluesky still shows my old handle.** Other services cache handles for a while. We tell the network as soon as your domain goes live, but it can take some time to appear everywhere.
