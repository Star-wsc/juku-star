package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	badNameChars = regexp.MustCompile(`[\\/\?%\*:|"<>\x00-\x1f]+`)
	spaceChars   = regexp.MustCompile(`\s+`)
	rePublicURL  = regexp.MustCompile(`(?i)(?:https?|socks5h?)://[^\s"'<>]+`)
)

func publicError(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(redactErrorString(err.Error()))
}

func redactErrorString(s string) string {
	return rePublicURL.ReplaceAllStringFunc(s, func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil {
			return "[无效 URL 已隐藏]"
		}
		if u.Host == "" {
			return raw
		}
		if u.User != nil {
			u.User = url.User("redacted")
		}
		if u.RawQuery != "" {
			u.RawQuery = "[redacted]"
		}
		if u.Fragment != "" {
			u.Fragment = "redacted"
		}
		return u.String()
	})
}

func saveFailures(outputDir string, results []Result) error {
	var failed []Result
	for _, r := range results {
		if !r.OK {
			failed = append(failed, r)
		}
	}
	if len(failed) == 0 {
		return nil
	}
	b, err := json.MarshalIndent(failed, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outputDir, "failed.json"), b, 0o644)
}

func existingGood(path string, skipBytes int64) (bool, int64) {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return false, 0
	}
	return st.Size() >= skipBytes, st.Size()
}

func safeFilename(name string) string {
	name = strings.TrimSpace(name)
	name = badNameChars.ReplaceAllString(name, "_")
	name = strings.ReplaceAll(name, "..", "_")
	name = spaceChars.ReplaceAllString(name, " ")
	name = strings.Trim(name, " ._")
	if name == "" {
		return "未命名"
	}
	upper := strings.ToUpper(name)
	reserved := map[string]bool{"CON": true, "PRN": true, "AUX": true, "NUL": true, "COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true, "LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true}
	if reserved[strings.SplitN(upper, ".", 2)[0]] {
		name = "_" + name
	}
	runes := []rune(name)
	if len(runes) > 100 {
		name = string(runes[:100])
	}
	return name
}

func safeJoin(base string, elem ...string) (string, error) {
	cleanBase, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	parts := []string{cleanBase}
	for _, e := range elem {
		parts = append(parts, safeFilename(e))
	}
	joined := filepath.Join(parts...)
	abs, err := filepath.Abs(joined)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(cleanBase, abs)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("unsafe path outside output dir: %s", abs)
	}
	return abs, nil
}

func padEpisode(ep string) string {
	ep = strings.TrimSpace(ep)
	if n, err := strconv.Atoi(ep); err == nil {
		return fmt.Sprintf("%03d", n)
	}
	return safeFilename(ep)
}

func hashShort(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])[:12]
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
