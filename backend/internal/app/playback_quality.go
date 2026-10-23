package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type playbackQualityKey struct{}

type playbackQualityOption struct {
	Value int    `json:"value"`
	Label string `json:"label"`
}

var playbackHLSResolution = regexp.MustCompile(`(?:^|,)RESOLUTION=(\d+)x(\d+)(?:,|$)`)

func parsePlaybackQuality(value string) (int, error) {
	if value == "" || value == "auto" {
		return 0, nil
	}
	quality, err := strconv.Atoi(value)
	if err != nil || quality < 0 || quality > 4320 {
		return 0, errors.New("清晰度选项无效")
	}
	return quality, nil
}

func playbackQualityOptions(media providerMedia) []playbackQualityOption {
	options := make([]playbackQualityOption, 0, len(media.Variants))
	seen := make(map[int]bool)
	for _, variant := range media.Variants {
		if variant.Quality <= 0 || variant.Quality > 4320 || seen[variant.Quality] {
			continue
		}
		seen[variant.Quality] = true
		options = append(options, playbackQualityOption{Value: variant.Quality, Label: strconv.Itoa(variant.Quality) + "p"})
	}
	sort.Slice(options, func(i, j int) bool { return options[i].Value > options[j].Value })
	return options
}

func setPlaybackQualityHeaders(header http.Header, media providerMedia) {
	options, _ := json.Marshal(playbackQualityOptions(media))
	header.Set("X-Playback-Qualities", string(options))
	header.Set("X-Playback-Quality", strconv.Itoa(media.Quality))
}

func (downloader *Downloader) selectPlaybackQuality(ctx context.Context, media providerMedia) (providerMedia, error) {
	requested, _ := ctx.Value(playbackQualityKey{}).(int)
	if requested > 0 {
		for _, variant := range media.Variants {
			if variant.Quality == requested {
				variant.Variants = media.Variants
				media = variant
				break
			}
		}
	}
	lines := strings.Split(media.Playlist, "\n")
	variants := playbackHLSVariants(media.Playlist)
	if len(variants) == 0 {
		return media, nil
	}
	sort.SliceStable(variants, func(i, j int) bool { return variants[i].quality > variants[j].quality })
	selected := variants[0]
	media.Variants = nil
	for _, variant := range variants {
		media.Variants = append(media.Variants, providerMedia{Quality: variant.quality})
		if variant.quality == requested {
			selected = variant
		}
	}
	base, err := url.Parse(media.URL)
	if err != nil {
		return providerMedia{}, err
	}
	reference, err := url.Parse(selected.uri)
	if err != nil {
		return providerMedia{}, err
	}
	address := base.ResolveReference(reference).String()
	if !isProviderHTTPMediaURL(address) {
		return providerMedia{}, errors.New("清晰度播放列表地址无效")
	}
	playlist, err := downloader.fetchProviderText(ctx, address, media.Referer)
	if err != nil {
		return providerMedia{}, err
	}
	if !strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(playlist, "\ufeff")), "#EXTM3U") {
		return providerMedia{}, errors.New("站点未返回所选清晰度的播放列表")
	}
	filtered := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "#EXT-X-STREAM-INF:") && i+1 < len(lines) {
			if i == selected.line {
				filtered = append(filtered, lines[i], lines[i+1])
			}
			i++
			continue
		}
		if !strings.HasPrefix(line, "#EXT-X-I-FRAME-STREAM-INF:") {
			filtered = append(filtered, lines[i])
		}
	}
	media.Playlist, media.Quality = strings.Join(filtered, "\n"), selected.quality
	if duration := m3u8Duration(playlist); duration > 0 {
		media.Duration = duration
	}
	return media, nil
}

type playbackHLSVariant struct {
	line    int
	quality int
	uri     string
}

func playbackHLSVariants(playlist string) []playbackHLSVariant {
	lines := strings.Split(playlist, "\n")
	var variants []playbackHLSVariant
	for i := 0; i+1 < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
			continue
		}
		match := playbackHLSResolution.FindStringSubmatch(strings.TrimPrefix(line, "#EXT-X-STREAM-INF:"))
		if len(match) != 3 {
			continue
		}
		width, _ := strconv.Atoi(match[1])
		height, _ := strconv.Atoi(match[2])
		if width < height {
			height = width
		}
		uri := strings.TrimSpace(lines[i+1])
		if height > 0 && height <= 4320 && uri != "" && !strings.HasPrefix(uri, "#") {
			variants = append(variants, playbackHLSVariant{line: i, quality: height, uri: uri})
		}
	}
	return variants
}
