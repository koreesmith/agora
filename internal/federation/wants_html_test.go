package federation

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// AGORA-411: a shared link to a post on another Agora instance is that
// instance's ActivityPub object URL. A browser following it must land on the
// readable post page, and federation software must keep getting JSON.
func TestWantsHTML(t *testing.T) {
	cases := map[string]bool{
		"text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8": true,
		"application/activity+json": false,
		`application/ld+json; profile="https://www.w3.org/ns/activitystreams"`: false,
		"application/activity+json, text/html;q=0.1":                          false,
		"application/json": false,
		"":                 false,
	}
	for accept, want := range cases {
		r := httptest.NewRequest(http.MethodGet, "/federation/users/a/posts/b", nil)
		r.Header.Set("Accept", accept)
		if got := wantsHTML(r); got != want {
			t.Errorf("wantsHTML(%q) = %v, want %v", accept, got, want)
		}
	}
}
