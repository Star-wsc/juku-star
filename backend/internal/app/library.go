package app

import (
	"fmt"
	"sort"
	"strings"
)

type libraryLoadError struct {
	failures map[string]error
}

func (failure *libraryLoadError) Error() string {
	var messages []string
	for source, cause := range failure.failures {
		messages = append(messages, fmt.Sprintf("%s 获取失败: %v", source, publicError(cause)))
	}
	sort.Strings(messages)
	return strings.Join(messages, "\n")
}

func dramaProvider(drama Drama) string {
	if source := canonicalProviderSource(drama.Source); source != "" {
		return source
	}
	if source, _, ok := splitProviderDramaID(drama.ID); ok {
		return source
	}
	return "cloudfront"
}

func mergeLoadedDramas(cached, fresh []Drama, loadErr error) []Drama {
	return mergeSourceDramas(cached, fresh, loadErr, "")
}

func matchesSourceFilter(provider, source string) bool {
	if source == "" {
		return true
	}
	if source == "huangguo" {
		return provider == "cloudfront" || provider == sourceHuangguoAI || provider == sourceHuangguoVideo
	}
	return provider == source || provider == canonicalProviderSource(source)
}

func mergeSourceDramas(cached, fresh []Drama, loadErr error, source string) []Drama {
	merged := make([]Drama, 0, len(cached)+len(fresh))
	positions := make(map[string]int, cap(merged))
	for _, batch := range [][]Drama{cached, fresh} {
		for _, drama := range batch {
			if drama.ID == "" {
				continue
			}
			if position, found := positions[drama.ID]; found {
				merged[position] = mergeDramaMetadata(drama, merged[position])
			} else {
				positions[drama.ID] = len(merged)
				merged = append(merged, drama)
			}
		}
	}
	sort.SliceStable(merged, func(left, right int) bool { return merged[left].DisplayTitle() < merged[right].DisplayTitle() })
	return merged
}
