package media

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// videoEdit (AGORA-439) is what the person asked for when they trimmed a clip
// or picked its cover frame, already checked against the video. The zero value
// means "post it as it is".
type videoEdit struct {
	Trimmed  bool
	StartSec float64 // where the clip starts in the original
	LenSec   float64 // how long it runs
	// CoverSec is the cover frame's time within the trimmed clip. Only
	// meaningful when HasCover.
	CoverSec float64
	HasCover bool
}

const (
	// A clip shorter than this is an upload mistake (the trim handles were
	// dragged together), and too short to transcode into anything playable.
	minClipSec = 0.5
	// Slack for a client that measured the video's length slightly differently
	// from ffprobe: an end up to this far past the real end is read as "to the
	// end" rather than rejected.
	endSlackSec = 0.5
)

// msField reads an optional whole number of milliseconds from an upload form.
func msField(name, raw string) (ms float64, present bool, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false, nil
	}
	n, perr := strconv.ParseInt(raw, 10, 64)
	if perr != nil || n < 0 {
		return 0, false, fmt.Errorf("%s must be a whole number of milliseconds, 0 or more", name)
	}
	return float64(n) / 1000, true, nil
}

// parseVideoEdit checks the optional trim_start_ms, trim_end_ms and cover_ms
// form fields against a video of rawDur seconds. maxSec is the longest clip
// allowed after trimming.
//
// The length limit applies to the trimmed clip, so a video over the limit can
// be uploaded as long as the part kept is within it. With no trim fields it is
// the whole video that has to fit, exactly as before this existed. A cover
// time outside the clip is not an error: the usual frame is used instead.
func parseVideoEdit(startRaw, endRaw, coverRaw string, rawDur, maxSec float64) (videoEdit, error) {
	start, hasStart, err := msField("trim_start_ms", startRaw)
	if err != nil {
		return videoEdit{}, err
	}
	end, hasEnd, err := msField("trim_end_ms", endRaw)
	if err != nil {
		return videoEdit{}, err
	}
	cover, hasCover, err := msField("cover_ms", coverRaw)
	if err != nil {
		return videoEdit{}, err
	}

	var e videoEdit
	clipStart, clipEnd := 0.0, rawDur

	if hasStart || hasEnd {
		if hasStart {
			clipStart = start
		}
		if hasEnd {
			clipEnd = end
		}
		if clipStart >= rawDur {
			return videoEdit{}, fmt.Errorf("the trim starts after the end of the video (it is %.1f seconds long)", rawDur)
		}
		if clipEnd > rawDur {
			if clipEnd-rawDur > endSlackSec {
				return videoEdit{}, fmt.Errorf("the trim ends past the end of the video (it is %.1f seconds long)", rawDur)
			}
			clipEnd = rawDur
		}
		if clipEnd <= clipStart {
			return videoEdit{}, fmt.Errorf("the trim end must be after the trim start")
		}
		if clipEnd-clipStart < minClipSec {
			return videoEdit{}, fmt.Errorf("the clip must be at least half a second long")
		}
		// Keeping the whole video is not a trim: leave it to the usual path.
		if clipStart > 0 || clipEnd < rawDur-endSlackSec {
			e = videoEdit{Trimmed: true, StartSec: clipStart, LenSec: clipEnd - clipStart}
		}
	}

	length := rawDur
	if e.Trimmed {
		length = e.LenSec
	}
	if length > maxSec {
		if e.Trimmed {
			return videoEdit{}, fmt.Errorf("video is too long (max %s — the part you kept is %.0f seconds)", limitText(maxSec), math.Round(length))
		}
		return videoEdit{}, fmt.Errorf("video is too long (max %s — this video is %.0f seconds)", limitText(maxSec), math.Round(length))
	}

	if hasCover && cover < length {
		e.CoverSec, e.HasCover = cover, true
	}
	return e, nil
}

// limitText says a length limit the way a person would: "2 minutes",
// "90 seconds". The limit is the admin's to set (AGORA-444).
func limitText(sec float64) string {
	n := int(math.Round(sec))
	if n >= 60 && n%60 == 0 {
		if n == 60 {
			return "1 minute"
		}
		return fmt.Sprintf("%d minutes", n/60)
	}
	return fmt.Sprintf("%d seconds", n)
}
