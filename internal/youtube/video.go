package youtube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	reISODuration = regexp.MustCompile(`^PT(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?$`)

	apiURL = "https://www.googleapis.com/youtube/v3/videos"
)

// IsShort reports whether a URL is a YouTube Shorts video.
func IsShort(link string) bool {
	return strings.Contains(link, "youtube.com/shorts/")
}

// VideoInfo holds Data API fields used to render a meta line.
type VideoInfo struct {
	Duration       time.Duration
	LiveStatus     string    // "none" | "upcoming" | "live"
	ScheduledStart time.Time // zero unless LiveStatus == "upcoming"
	Stream         bool      // a broadcast
}

// MetaLine renders duration, scheduled time, or LIVE tag.
func (v *VideoInfo) MetaLine() string {
	switch v.LiveStatus {
	case "live":
		return "🔴 LIVE"
	case "upcoming":
		if v.ScheduledStart.IsZero() {
			return ""
		}
		return "📅 <code>" + v.ScheduledStart.UTC().Format("2006-01-02 15:04") + " UTC</code>"
	case "none":
		if v.Duration <= 0 {
			return ""
		}
		return "⏱️ <code>" + formatDuration(v.Duration) + "</code>"
	}
	return ""
}

// ExtractVideoID returns the video ID from a watch, shorts, or youtu.be URL.
func ExtractVideoID(link string) (string, bool) {
	u, err := url.Parse(link)
	if err != nil {
		return "", false
	}
	switch normalizeHost(u.Host) {
	case "youtube.com":
		if id := u.Query().Get("v"); id != "" {
			return id, true
		}
		if id, ok := strings.CutPrefix(u.Path, "/shorts/"); ok {
			id, _, _ = strings.Cut(id, "/")
			if id != "" {
				return id, true
			}
		}
	case "youtu.be":
		if id := strings.TrimPrefix(u.Path, "/"); id != "" {
			return id, true
		}
	}
	return "", false
}

// FetchVideoInfo queries the Data API v3 for a single video.
func FetchVideoInfo(ctx context.Context, apiKey, videoID string) (*VideoInfo, error) {
	q := url.Values{
		"part": {"snippet,contentDetails,liveStreamingDetails"},
		"id":   {videoID},
		"key":  {apiKey},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL+"?"+q.Encode(), http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching video info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching video info: status %d", resp.StatusCode)
	}

	var body struct {
		Items []struct {
			Snippet struct {
				LiveBroadcastContent string `json:"liveBroadcastContent"`
			} `json:"snippet"`
			ContentDetails struct {
				Duration string `json:"duration"`
			} `json:"contentDetails"`
			LiveStreamingDetails *struct {
				ScheduledStartTime time.Time `json:"scheduledStartTime"`
			} `json:"liveStreamingDetails"`
		} `json:"items"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBody)).Decode(&body); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	if len(body.Items) == 0 {
		return nil, errors.New("video not found")
	}

	item := body.Items[0]
	info := &VideoInfo{
		Duration:   parseISODuration(item.ContentDetails.Duration),
		LiveStatus: item.Snippet.LiveBroadcastContent,
	}
	if d := item.LiveStreamingDetails; d != nil {
		info.Stream = true
		info.ScheduledStart = d.ScheduledStartTime
	}
	return info, nil
}

func parseISODuration(s string) time.Duration {
	m := reISODuration.FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	h, _ := strconv.Atoi(m[1])
	mins, _ := strconv.Atoi(m[2])
	secs, _ := strconv.Atoi(m[3])
	return time.Duration(h)*time.Hour + time.Duration(mins)*time.Minute + time.Duration(secs)*time.Second
}

func formatDuration(d time.Duration) string {
	total := int(d.Seconds())
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}
