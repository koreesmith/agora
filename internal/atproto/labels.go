package atproto

import (
	"strings"

	comatproto "github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/api/bsky"
)

// blueskyContentWarningLabel is the self-label value Agora emits for any
// non-empty content_warning (AGORA-204). Deliberately not one of Bluesky's
// own well-known adult-content categories (sexual/nudity/graphic-media/
// porn) — those carry a specific real meaning to Bluesky's client and any
// moderation tooling keyed off them, and Agora's content_warning is a
// free-text field that can mean anything ("spoilers: series finale",
// "discussion of illness", etc.), not necessarily adult content. A distinct
// custom value avoids misrepresenting the post under an unrelated category;
// self-labels are a plain string per the lexicon (LabelDefs_SelfLabel.Val),
// not a closed enum, so an app-specific value is valid AT Proto, even if
// Bluesky's own client has no special rendering for it. This is the
// best-effort mapping AGORA-204 explicitly accepts: the specific CW *text*
// doesn't survive the trip (a closed label vocabulary has no free-text
// component), only the fact that a warning exists at all.
const blueskyContentWarningLabel = "agora-content-warning"

// labelsForContentWarning builds the record-level Labels field for an
// outbound post/reply, or nil if there's no content warning to carry over.
func labelsForContentWarning(contentWarning string) *bsky.FeedPost_Labels {
	if contentWarning == "" {
		return nil
	}
	return &bsky.FeedPost_Labels{
		LabelDefs_SelfLabels: &comatproto.LabelDefs_SelfLabels{
			Values: []*comatproto.LabelDefs_SelfLabel{{Val: blueskyContentWarningLabel}},
		},
	}
}

// contentWarningFromLabels is labelsForContentWarning's inverse for inbound
// ingestion (AGORA-204's other half) — a Bluesky post's self-labels have no
// free-text component, so the best-effort mapping surfaces the label
// name(s) themselves as Agora's content_warning display text rather than
// silently dropping them (the exact failure mode AGORA-154 already hit once
// on the fediverse side and this epic must not repeat). Recognizes both
// Agora's own outbound label and Bluesky's well-known adult-content
// categories, so a warning applied by a real Bluesky client (not just by
// another Agora instance) still surfaces as something rather than nothing.
func contentWarningFromLabels(labels *bsky.FeedPost_Labels) string {
	if labels == nil || labels.LabelDefs_SelfLabels == nil {
		return ""
	}
	var vals []string
	for _, v := range labels.LabelDefs_SelfLabels.Values {
		if v == nil || v.Val == "" {
			continue
		}
		if v.Val == blueskyContentWarningLabel {
			vals = append(vals, "content warning")
			continue
		}
		vals = append(vals, v.Val)
	}
	if len(vals) == 0 {
		return ""
	}
	return "Bluesky content label: " + strings.Join(vals, ", ")
}
