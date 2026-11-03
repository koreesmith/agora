package media

import "testing"

// AGORA-437: parseProbe turns ffprobe's JSON into a video's display size and
// length.
func TestParseProbe(t *testing.T) {
	cases := []struct {
		name string
		json string
		want VideoMeta
		err  bool
	}{
		{"plain landscape", `{"streams":[{"width":1280,"height":720}],"format":{"duration":"65.480000"}}`,
			VideoMeta{1280, 720, 65480}, false},
		{"vertical", `{"streams":[{"width":720,"height":1280}],"format":{"duration":"12.5"}}`,
			VideoMeta{720, 1280, 12500}, false},
		{"rotate tag 90 swaps", `{"streams":[{"width":1920,"height":1080,"tags":{"rotate":"90"}}],"format":{"duration":"3"}}`,
			VideoMeta{1080, 1920, 3000}, false},
		{"side data -90 swaps", `{"streams":[{"width":1920,"height":1080,"side_data_list":[{"rotation":-90}]}],"format":{"duration":"3"}}`,
			VideoMeta{1080, 1920, 3000}, false},
		{"rotate 180 does not swap", `{"streams":[{"width":1920,"height":1080,"tags":{"rotate":"180"}}],"format":{"duration":"3"}}`,
			VideoMeta{1920, 1080, 3000}, false},
		{"no duration still has the size", `{"streams":[{"width":640,"height":360}],"format":{}}`,
			VideoMeta{640, 360, 0}, false},
		{"audio only is an error", `{"streams":[],"format":{"duration":"10"}}`, VideoMeta{}, true},
		{"zero size is an error", `{"streams":[{"width":0,"height":0}],"format":{"duration":"10"}}`, VideoMeta{}, true},
		{"not json is an error", `nope`, VideoMeta{}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseProbe([]byte(c.json))
			if (err != nil) != c.err {
				t.Fatalf("err = %v, want error: %v", err, c.err)
			}
			if got != c.want {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}
