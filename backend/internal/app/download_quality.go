package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
)

func readDownloadRequest(writer http.ResponseWriter, request *http.Request) ([]string, *int, bool) {
	var input struct {
		IDs     []string `json:"ids"`
		Quality *int     `json:"quality"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "下载参数无效"})
		return nil, nil, false
	}
	if decoder.Decode(new(any)) != io.EOF || input.Quality != nil && (*input.Quality < 0 || *input.Quality > 4320) {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "下载清晰度选项无效"})
		return nil, nil, false
	}
	ids, ok := cleanIDList(writer, input.IDs)
	return ids, input.Quality, ok
}

func preferredDownloadQuality(requested int, available []int) int {
	best, lowest := 0, 0
	for _, quality := range available {
		if quality <= 0 || quality > 4320 {
			continue
		}
		if lowest == 0 || quality < lowest {
			lowest = quality
		}
		if (requested <= 0 || quality <= requested) && quality > best {
			best = quality
		}
	}
	if best == 0 {
		return lowest
	}
	return best
}

func (downloader *Downloader) selectDownloadQuality(ctx context.Context, task Task, media providerMedia) (providerMedia, error) {
	qualities := []int{media.Quality}
	validVariants := make([]providerMedia, 0, len(media.Variants)+1)
	for _, variant := range media.Variants {
		if isProviderHTTPMediaURL(variant.URL) {
			qualities = append(qualities, variant.Quality)
			validVariants = append(validVariants, variant)
		}
	}
	if isProviderHTTPMediaURL(media.URL) {
		current := media
		current.Variants = nil
		validVariants = append(validVariants, current)
	}
	media.Variants = validVariants
	if variants := playbackHLSVariants(media.Playlist); len(variants) > 0 {
		qualities = nil
		for _, variant := range variants {
			qualities = append(qualities, variant.quality)
		}
	}
	quality := preferredDownloadQuality(task.DownloadQuality, qualities)
	selected, err := downloader.selectPlaybackQuality(context.WithValue(ctx, playbackQualityKey{}, quality), media)
	if err != nil {
		return providerMedia{}, err
	}
	var available []int
	for _, option := range playbackQualityOptions(selected) {
		available = append(available, option.Value)
	}
	downloader.recordDiagnostic(diagnosticEvent{Level: "info", Event: "download.media", Source: sourceFromDramaID(task.DramaID),
		DramaID: task.DramaID, DramaTitle: task.DramaTitle, Episode: task.Index, Quality: selected.Quality, RequestedQuality: task.DownloadQuality, AvailableQualities: available,
		Message: "下载保留源媒体的音视频编码和分辨率"})
	return selected, nil
}
