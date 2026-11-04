# ADR-003: Suggested Alt Text for Photos

**Status:** Proposed  
**Ticket:** AGORA-415  
**Date:** 2026-11-04  
**Superseded by:** —

---

## Context

Agora stores image descriptions (alt text) but never writes them. `internal/alttext` trims, caps at 1500 characters (Mastodon's limit) and pads, for descriptions a person typed in the composer (AGORA-400) or that arrived with a post from the Fediverse or Bluesky. A photo posted without a description has empty alt text, which federates as such.

The question is whether Agora should help people write one, and how, without breaking the project's constraints:

- Agora is **self-hosted** by operators, often on one small server with no GPU. Nothing in the stack today calls a paid third-party API for core features.
- Posts have audiences (public, friends, groups, private). A photo that is not public must not leave the instance without the author knowing.
- Descriptions go out to the Fediverse and Bluesky with the post, so wrong text is published under the author's name.

## Options considered

### A. Fill the description in automatically on every post

Rejected. Generated captions are sometimes wrong (people, text, symbols, context), and the Fediverse community expects descriptions to be accurate and checked by a human; many accessibility advocates consider a wrong description worse than none. Writing one without the author seeing it would publish unreviewed text under their name. It would also send every photo of every post, including friends-only ones, to the model without the author asking.

### B. A suggestion the author reviews, from a model the operator runs (recommended)

A "Suggest a description" button in the alt text dialog on web and mobile. The author taps it, the server asks a vision model to describe the image, the text fills the box, and the author edits it and saves it like any typed description. Nothing is stored or posted until they do.

### C. A suggestion from a third-party API

Same experience as B, with the model being a hosted service. Better quality for the least operator effort, but the photo leaves the instance. Allowed only as an explicit operator choice (see Privacy), never the default.

### D. OCR only

Tesseract-style text extraction helps for screenshots and memes and nothing for photographs. Not worth building alone. A vision model that is asked to include visible text covers it.

### E. Backfill descriptions on existing photos

Rejected for now. It would put machine-written text on posts people already published and never reviewed, and it is the case where the privacy question is worst. The suggestion button can be offered when someone edits an old post's descriptions (AGORA-400 already allows editing).

## Decision

Build **B**, with **C** available as an operator setting, off by default. Do not build A, D or E.

### How it works

- A new endpoint, `POST /api/media/describe`, takes an image URL the caller has already uploaded and returns `{ "description": "..." }`. It requires login and is rate limited per account (the signup and auth code already has a limiter to follow).
- The server resizes the image to fit 1024 px on the long side, sends it to the configured model, and trims and caps the reply with `alttext.Clean` so it obeys the same 1500 character limit as typed text.
- The prompt asks for one or two plain sentences, no opening like "This image shows", and any visible text quoted. It asks for the description in the instance's language setting where there is one.
- The endpoint has a timeout. If the model is slow or down, the button reports it and the author types their own description. Posting is never blocked on it.
- The result is a suggestion only. The dialog shows the usual field filled in, so the author reads it before saving.
- Nothing about the description being machine-written is stored or federated. Once the author has reviewed and saved it, it is their description. The dialog does say it was suggested and should be checked.

### Providers

One setting group in the admin area, all off until configured:

| Setting | Meaning |
|---|---|
| `alt_text_suggestions_enabled` | Master switch. Off by default. The button does not appear when off. |
| `alt_text_endpoint` | URL of an OpenAI-compatible or Ollama chat endpoint that accepts an image. |
| `alt_text_model` | Model name sent to it. |
| `alt_text_api_key` | Optional, for a hosted endpoint. Stored like the other instance secrets. |
| `alt_text_external` | The operator confirms the endpoint is outside their own infrastructure. Required before a hosted endpoint is accepted, with the privacy text below shown beside it. |

Speaking one common protocol (an OpenAI-style `/v1/chat/completions` with an image part, which Ollama and most hosted services accept) means one code path serves a local model and a hosted one.

**Suggested self-hosted model:** [Moondream 2](https://inference-models.roboflow.com/models/moondream2/) (about 1.9B parameters, Apache 2.0, runs on x86 CPU and small GPUs) through Ollama. It is small enough for a self-hoster's server. Larger models such as Qwen2.5-VL or MiniCPM-V read text better but want a GPU. We have not measured captioning speed on CPU, so latency on a typical Agora host must be measured before we promise a time in the docs. The docs should present it as "works with any compatible model", not as a requirement.

### Privacy

- The image is sent only when the author taps the button, never in the background and never because a post was made.
- Because the button is used before the post exists, the audience is not yet known, so the rule is about where the model lives rather than the visibility: a model the operator runs on their own infrastructure may see any photo an author asks about; a hosted one is allowed only when the operator has set `alt_text_external` and the admin screen has told them that the photos authors ask about are sent to that service.
- The settings text and the user docs say this plainly. The dialog on an instance with an external endpoint says "The photo will be sent to <service> to write a suggestion."
- Nothing is logged except that a request was made. The image and the reply are not kept.
- No provider is contacted when the master switch is off, and no photo is sent to anything by default.

### Federation

Reviewed descriptions federate exactly as typed ones do today (ActivityPub `attachment[].name`, Bluesky image `alt`). No change.

### Accuracy and safety

- The author must read it. The suggestion is never submitted without the dialog being saved.
- Sensitive photos: a content warning on the post is unaffected, and the model is asked only to describe, not to judge. Operators who do not want certain content sent anywhere simply leave the feature off or run a local model.
- Length: the cap is enforced by `alttext.Clean`; the prompt asks for short text, so most suggestions are well under it.

## Consequences

- Authors who use a screen reader or care about accessibility get a head start, and photos are more likely to have a description. People who do not tap the button get no change.
- Operators who do nothing see no change and no new dependency. Operators who want it run Ollama (or point at a hosted service) and fill in four settings.
- Quality will vary. The community norm is that machine-written text must be checked, which is why this is a suggestion and not an automatic fill.
- We carry a new outbound HTTP call, a rate limit and four settings to maintain.

## Rough effort

About four stories, three to four days in total, not counting measuring a model on real hardware:

1. **Server:** `POST /api/media/describe`, the provider call, resize, limits, tests with a fake endpoint (about 1.5 days).
2. **Admin:** the five settings with validation and the external-endpoint notice, and storage of the key (about 0.5 day).
3. **Web and mobile:** the button and notice in the alt text dialogs (about 1 day).
4. **Docs and measurement:** user and operator pages, a recommended local model, and measured CPU and GPU latency (about 0.5 to 1 day).

## Open questions

- Which model do we recommend after measuring it on a small CPU-only host? If the answer is "too slow", the docs lead with a GPU or hosted option instead.
- Should an admin be able to cap suggestions per day, for cost on a hosted endpoint? A per-account limit is in the design; an instance-wide one is cheap to add.
- Should videos get the same treatment later (poster frame description)? Out of scope here.
