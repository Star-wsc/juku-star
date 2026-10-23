package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func normalizedFFmpegSetting(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		value = strings.TrimSpace(value[1 : len(value)-1])
	}
	return value
}

func automaticFFmpeg(value string) bool {
	value = normalizedFFmpegSetting(value)
	if runtime.GOOS == "windows" {
		value = strings.ToLower(value)
	}
	return value == "" || value == "ffmpeg" || value == "ffmpeg.exe"
}

func ffmpegExecutableName() string {
	if runtime.GOOS == "windows" {
		return "ffmpeg.exe"
	}
	return "ffmpeg"
}

func ffmpegManagedDirectory() string {
	directory := filepath.Join("bin", runtime.GOOS+"-"+runtime.GOARCH, "verified-b6.1.1")
	if absolute, err := filepath.Abs(directory); err == nil {
		return absolute
	}
	return directory
}

func ffmpegCandidates(configured, managedDirectory string) []string {
	var candidates []string
	seen := make(map[string]bool)
	add := func(candidate string) {
		path, err := exec.LookPath(candidate)
		if err != nil {
			return
		}
		path, err = filepath.Abs(path)
		if err != nil {
			return
		}
		key := path
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		if !seen[key] {
			seen[key] = true
			candidates = append(candidates, path)
		}
	}
	configured = normalizedFFmpegSetting(configured)
	if !automaticFFmpeg(configured) {
		add(configured)
		return candidates
	}
	name := ffmpegExecutableName()
	for _, directory := range filepath.SplitList(os.Getenv("PATH")) {
		if filepath.IsAbs(directory) {
			add(filepath.Join(directory, name))
		}
	}
	for _, path := range []string{filepath.Join("bin", name), name, filepath.Join(managedDirectory, name), filepath.Join("bin", runtime.GOOS+"-"+runtime.GOARCH, name)} {
		if absolute, err := filepath.Abs(path); err == nil {
			add(absolute)
		}
	}
	return candidates
}
