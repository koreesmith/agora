package federation

import "testing"

// AGORA-175: threads.net sends Accept(Follow)'s "object" as a bare IRI
// string (the original Follow's id) instead of embedding the Follow object,
// which used to make handleInboundAcceptFollow silently no-op and leave the
// follow stuck on "Requested" forever.
func TestUsernameFromAcceptObject(t *testing.T) {
	const domain = "https://agora.example"

	cases := []struct {
		name   string
		object string
		want   string
	}{
		{
			name:   "embedded Follow object (Mastodon-style)",
			object: `{"type":"Follow","actor":"https://agora.example/federation/users/alice","object":"https://threads.net/users/bob"}`,
			want:   "alice",
		},
		{
			name:   "bare IRI string referencing the original Follow's id (threads.net-style)",
			object: `"https://agora.example/federation/users/alice/follows/1699999999"`,
			want:   "alice",
		},
		{
			name:   "embedded object with empty actor falls back to string parse and fails",
			object: `{"type":"Follow","actor":""}`,
			want:   "",
		},
		{
			name:   "IRI string not belonging to our instance",
			object: `"https://threads.net/users/bob/follows/1"`,
			want:   "",
		},
		{
			name:   "garbage",
			object: `42`,
			want:   "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := usernameFromAcceptObject([]byte(tc.object), domain)
			if got != tc.want {
				t.Errorf("usernameFromAcceptObject(%s) = %q, want %q", tc.object, got, tc.want)
			}
		})
	}
}
