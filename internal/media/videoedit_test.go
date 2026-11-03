package media

import (
	"strings"
	"testing"
)

// AGORA-439: the trim and cover fields on a video upload.
func TestParseVideoEdit(t *testing.T) {
	const max = 120.0
	cases := []struct {
		name              string
		start, end, cover string
		rawDur            float64
		want              videoEdit
		errContains       string
	}{
		{name: "no fields is posted as it is", rawDur: 30, want: videoEdit{}},
		{name: "trim start and end", start: "5000", end: "12000", rawDur: 30,
			want: videoEdit{Trimmed: true, StartSec: 5, LenSec: 7}},
		{name: "start only runs to the end", start: "10000", rawDur: 30,
			want: videoEdit{Trimmed: true, StartSec: 10, LenSec: 20}},
		{name: "end only starts at the beginning", end: "8000", rawDur: 30,
			want: videoEdit{Trimmed: true, StartSec: 0, LenSec: 8}},
		{name: "keeping everything is not a trim", start: "0", end: "30000", rawDur: 30, want: videoEdit{}},
		{name: "end a hair past the real end counts as the end", start: "0", end: "30300", rawDur: 30, want: videoEdit{}},
		{name: "a trim that keeps the end, clamped", start: "10000", end: "30300", rawDur: 30,
			want: videoEdit{Trimmed: true, StartSec: 10, LenSec: 20}},
		{name: "empty range", start: "5000", end: "5000", rawDur: 30, errContains: "after the trim start"},
		{name: "inverted range", start: "9000", end: "4000", rawDur: 30, errContains: "after the trim start"},
		{name: "start past the end", start: "31000", rawDur: 30, errContains: "starts after the end"},
		{name: "end far past the end", start: "1000", end: "40000", rawDur: 30, errContains: "ends past the end"},
		{name: "too short to be a clip", start: "1000", end: "1200", rawDur: 30, errContains: "half a second"},
		{name: "negative", start: "-5", rawDur: 30, errContains: "trim_start_ms"},
		{name: "not a number", end: "soon", rawDur: 30, errContains: "trim_end_ms"},
		{name: "decimal is not whole milliseconds", start: "1500.5", rawDur: 30, errContains: "trim_start_ms"},

		// the limit applies to what is kept
		{name: "long video, untrimmed, is refused as before", rawDur: 150, errContains: "max 2 minutes — this video is 150 seconds"},
		{name: "long video trimmed to fit", start: "10000", end: "40000", rawDur: 150,
			want: videoEdit{Trimmed: true, StartSec: 10, LenSec: 30}},
		{name: "long video trimmed but still too long", start: "0", end: "130000", rawDur: 200, errContains: "the part you kept is 130 seconds"},
		{name: "exactly the limit is fine", start: "0", end: "120000", rawDur: 200,
			want: videoEdit{Trimmed: true, StartSec: 0, LenSec: 120}},

		// cover frame
		{name: "cover inside the clip", start: "5000", end: "12000", cover: "3000", rawDur: 30,
			want: videoEdit{Trimmed: true, StartSec: 5, LenSec: 7, CoverSec: 3, HasCover: true}},
		{name: "cover with no trim", cover: "2500", rawDur: 30, want: videoEdit{CoverSec: 2.5, HasCover: true}},
		{name: "cover at the very start", cover: "0", rawDur: 30, want: videoEdit{CoverSec: 0, HasCover: true}},
		{name: "cover past the end of the clip falls back", start: "5000", end: "12000", cover: "7000", rawDur: 30,
			want: videoEdit{Trimmed: true, StartSec: 5, LenSec: 7}},
		{name: "cover far outside falls back", cover: "999999999", rawDur: 30, want: videoEdit{}},
		{name: "cover that is not a number is an error", cover: "middle", rawDur: 30, errContains: "cover_ms"},
		{name: "cover is time within the trimmed clip, not the original", start: "20000", end: "25000", cover: "21000", rawDur: 30,
			want: videoEdit{Trimmed: true, StartSec: 20, LenSec: 5}}, // 21s is past a 5s clip
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseVideoEdit(c.start, c.end, c.cover, c.rawDur, max)
			if c.errContains != "" {
				if err == nil {
					t.Fatalf("want an error containing %q, got %+v", c.errContains, got)
				}
				if !strings.Contains(err.Error(), c.errContains) {
					t.Fatalf("error %q does not contain %q", err, c.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != c.want {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}
