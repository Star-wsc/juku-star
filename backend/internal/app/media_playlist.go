package app

import (
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func m3u8Duration(raw string) time.Duration {
	var total time.Duration
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "#EXTINF:") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, "#EXTINF:"))
		if comma := strings.IndexByte(value, ','); comma >= 0 {
			value = value[:comma]
		}
		seconds, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil || seconds < 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
			continue
		}
		total += time.Duration(seconds * float64(time.Second))
	}
	return total
}

func rewriteM3U8(raw, playlistURL string) string {
	base, err := url.Parse(playlistURL)
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "URI=") && strings.Contains(trimmed, "/api/app/vid/sec") {
			lines[i] = regexp.MustCompile(`URI="[^"]+"`).ReplaceAllString(line, `URI="key.bin"`)
			continue
		}
		if err == nil && trimmed != "" && !strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "http://") && !strings.HasPrefix(trimmed, "https://") {
			ref, refErr := url.Parse(trimmed)
			if refErr == nil {
				lines[i] = base.ResolveReference(ref).String()
			}
		}
	}
	return strings.Join(lines, "\n")
}
