package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// PlaybackSettings 对齐 2.0 playback-settings.json。
type PlaybackSettings struct {
	Enabled            bool     `json:"enabled"`
	DirectSources      []string `json:"directSources"`
	MaxSessions        int      `json:"maxSessions"`
	MaxMediaRequests   int      `json:"maxMediaRequests"`
	MaxRemuxJobs       int      `json:"maxRemuxJobs"`
	MaxAudioTranscodes int      `json:"maxAudioTranscodes"`
	MaxVideoTranscodes int      `json:"maxVideoTranscodes"`
	MaxPrefetchJobs    int      `json:"maxPrefetchJobs"`
	CacheMB            int      `json:"cacheMB"`
	BackgroundPlayback bool     `json:"backgroundPlayback"`
}

func defaultPlaybackSettings() PlaybackSettings {
	return PlaybackSettings{
		Enabled:            true,
		DirectSources:      []string{sourceHongguo},
		MaxSessions:        4,
		MaxMediaRequests:   32,
		MaxRemuxJobs:       2,
		MaxAudioTranscodes: 1,
		MaxVideoTranscodes: 1,
		MaxPrefetchJobs:    2,
		CacheMB:            512,
		BackgroundPlayback: true,
	}
}

func (s PlaybackSettings) normalized() PlaybackSettings {
	out := s
	if out.MaxSessions <= 0 {
		out.MaxSessions = 4
	}
	if out.MaxMediaRequests <= 0 {
		out.MaxMediaRequests = 32
	}
	if out.MaxRemuxJobs <= 0 {
		out.MaxRemuxJobs = 2
	}
	if out.MaxAudioTranscodes <= 0 {
		out.MaxAudioTranscodes = 1
	}
	if out.MaxVideoTranscodes <= 0 {
		out.MaxVideoTranscodes = 1
	}
	if out.MaxPrefetchJobs <= 0 {
		out.MaxPrefetchJobs = 2
	}
	if out.CacheMB <= 0 {
		out.CacheMB = 512
	}
	seen := map[string]bool{}
	var sources []string
	for _, raw := range out.DirectSources {
		src := canonicalProviderSource(strings.TrimSpace(raw))
		if src != "" && !seen[src] {
			seen[src] = true
			sources = append(sources, src)
		}
	}
	if len(sources) == 0 && out.Enabled {
		sources = []string{sourceHongguo}
	}
	out.DirectSources = sources
	return out
}

func (s PlaybackSettings) allowsDirectSource(source string) bool {
	if !s.Enabled {
		return false
	}
	source = canonicalProviderSource(source)
	for _, allowed := range s.DirectSources {
		if allowed == source {
			return true
		}
	}
	return false
}

type playbackSettingsStore struct {
	path string
	mu   sync.RWMutex
	cur  PlaybackSettings
}

func newPlaybackSettingsStore(dataDirectory string) *playbackSettingsStore {
	store := &playbackSettingsStore{path: filepath.Join(dataDirectory, "playback-settings.json"), cur: defaultPlaybackSettings()}
	store.load()
	return store
}

func (store *playbackSettingsStore) load() {
	store.mu.Lock()
	defer store.mu.Unlock()
	body, err := os.ReadFile(store.path)
	if err != nil {
		store.cur = defaultPlaybackSettings()
		return
	}
	var settings PlaybackSettings
	if err := json.Unmarshal(body, &settings); err != nil {
		store.cur = defaultPlaybackSettings()
		return
	}
	store.cur = settings.normalized()
}

func (store *playbackSettingsStore) get() PlaybackSettings {
	store.mu.RLock()
	defer store.mu.RUnlock()
	return store.cur
}

func (store *playbackSettingsStore) update(settings PlaybackSettings) PlaybackSettings {
	normalized := settings.normalized()
	store.mu.Lock()
	store.cur = normalized
	store.mu.Unlock()
	body, err := json.MarshalIndent(normalized, "", "  ")
	if err == nil {
		_ = os.WriteFile(store.path, append(body, '\n'), 0o600)
	}
	return normalized
}

type playbackCompatHints struct {
	VideoCodecUnsupported   bool   `json:"videoCodecUnsupported,omitempty"`
	AudioCodecUnsupported   bool   `json:"audioCodecUnsupported,omitempty"`
	ContainerCompatibility  string `json:"containerCompatibility,omitempty"`
	NeedDanmakuGuideType    string `json:"needDanmakuGuideType,omitempty"`
}

func (h playbackCompatHints) asMap() map[string]any {
	out := map[string]any{}
	if h.VideoCodecUnsupported {
		out["videoCodecUnsupported"] = true
	}
	if h.AudioCodecUnsupported {
		out["audioCodecUnsupported"] = true
	}
	if h.ContainerCompatibility != "" {
		out["containerCompatibility"] = h.ContainerCompatibility
	}
	if h.NeedDanmakuGuideType != "" {
		out["needDanmakuGuideType"] = h.NeedDanmakuGuideType
	}
	return out
}

func detectCompatHints(media providerMedia, mode, source string) playbackCompatHints {
	hints := playbackCompatHints{}
	container := ""
	if media.Playlist != "" || strings.Contains(strings.ToLower(media.URL), ".m3u8") {
		container = "hls"
	} else if strings.Contains(strings.ToLower(media.URL), ".mp4") || strings.Contains(strings.ToLower(media.URL), "video/mp4") {
		container = "mp4"
	} else if media.AudioURL != "" {
		container = "fmp4"
	}
	switch mode {
	case "original", "direct":
		if container == "" {
			container = "mp4"
		}
	case "compatible_original":
		if container == "" {
			container = "mp4"
		}
	case "hls":
		container = "hls"
	}
	hints.ContainerCompatibility = container
	if strings.Contains(strings.ToLower(media.URL), "bytevc2") || strings.Contains(strings.ToLower(media.Playlist), "bytevc2") {
		hints.VideoCodecUnsupported = true
	}
	if media.AudioURL != "" && mode != "direct" && mode != "original" && mode != "compatible_original" {
		// 分离音视频需要 remux，部分客户端不支持裸轨
		if container == "mp4" {
			hints.ContainerCompatibility = "fmp4"
		}
	}
	if source == "" {
		source = providerSourceForURL(media.URL)
	}
	if source == sourceHongguo {
		hints.NeedDanmakuGuideType = "hongguo"
	}
	return hints
}
