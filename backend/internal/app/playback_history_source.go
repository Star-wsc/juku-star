package app

import "strings"

func playbackHistoryIdentity(id string) (string, string, bool) {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > 512 || strings.ContainsAny(id, "\x00\r\n\t /\\?#") {
		return "", "", false
	}
	if source, sourceID, ok := splitProviderDramaID(id); ok {
		return providerDramaID(source, sourceID), source, true
	}
	if !strings.Contains(id, ":") {
		return id, "cloudfront", true
	}
	return "", "", false
}
