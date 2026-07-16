package federation

import "testing"

// AGORA-180: a threads.net post's video never showed up in the feed.
// Captured from a real inbound Create activity — threads.net attachments
// have no "mediaType" at all, just {"type":"Video","url":"...","width":...,
// "height":...}, so matching only on mediaType silently dropped every one.
func TestMatchAttachmentsThreadsVideoNoMediaType(t *testing.T) {
	attachments := []apAttachment{
		{Type: "Video", URL: "https://scontent.cdninstagram.com/o1/v/t16/f2/m84/example.mp4?efg=abc"},
	}

	imageURLs, videoURL := matchAttachments(attachments)

	if videoURL != attachments[0].URL {
		t.Errorf("videoURL = %q, want %q", videoURL, attachments[0].URL)
	}
	if len(imageURLs) != 0 {
		t.Errorf("imageURLs = %v, want empty", imageURLs)
	}
}

func TestMatchAttachmentsMediaTypeStillWorks(t *testing.T) {
	attachments := []apAttachment{
		{MediaType: "image/jpeg", URL: "https://example.com/a.jpg"},
		{MediaType: "video/mp4", URL: "https://example.com/v.mp4"},
	}

	imageURLs, videoURL := matchAttachments(attachments)

	if len(imageURLs) != 1 || imageURLs[0] != "https://example.com/a.jpg" {
		t.Errorf("imageURLs = %v, want [https://example.com/a.jpg]", imageURLs)
	}
	if videoURL != "https://example.com/v.mp4" {
		t.Errorf("videoURL = %q, want https://example.com/v.mp4", videoURL)
	}
}

func TestMatchAttachmentsOnlyFirstVideoKept(t *testing.T) {
	attachments := []apAttachment{
		{Type: "Video", URL: "https://example.com/first.mp4"},
		{Type: "Video", URL: "https://example.com/second.mp4"},
	}

	_, videoURL := matchAttachments(attachments)

	if videoURL != "https://example.com/first.mp4" {
		t.Errorf("videoURL = %q, want the first video kept", videoURL)
	}
}

func TestMatchAttachmentsNoURLIsSkipped(t *testing.T) {
	attachments := []apAttachment{
		{Type: "Video", URL: ""},
	}

	imageURLs, videoURL := matchAttachments(attachments)

	if videoURL != "" || len(imageURLs) != 0 {
		t.Errorf("expected nothing matched for an attachment with no URL, got imageURLs=%v videoURL=%q", imageURLs, videoURL)
	}
}
