package app

import "testing"

func TestPlaybackHistorySourceIdentity(t *testing.T) {
	for _, input := range []struct{ id, source string }{
		{"hongguo:7000000000000000001", "hongguo"},
		{"huangdou:123", "huangdou"},
		{"huangguoai:456", "huangguoai"},
		{"huangguo-video:789", "huangguo-video"},
		{"legacy-123", "cloudfront"},
	} {
		id, source, valid := playbackHistoryIdentity(input.id)
		if !valid || id != input.id || source != input.source {
			t.Fatal(input, id, source, valid)
		}
	}
	for _, invalid := range []string{"", "unsupported:123", "../data", "https://example.invalid", "123\n456"} {
		if _, _, valid := playbackHistoryIdentity(invalid); valid {
			t.Fatal("invalid history identity accepted", invalid)
		}
	}
}
