package app

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func portableFFmpeg(configured string) string {
	if candidates := ffmpegCandidates(configured, ffmpegManagedDirectory()); len(candidates) > 0 {
		return candidates[0]
	}
	if automaticFFmpeg(configured) {
		return ffmpegExecutableName()
	}
	return normalizedFFmpegSetting(configured)
}

func normalizedOutputDirectory(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("下载目录不能为空")
	}
	if strings.ContainsAny(raw, "\x00\r\n") {
		return "", errors.New("下载目录包含无效字符")
	}
	if strings.Contains(raw, "://") {
		return "", errors.New("下载目录必须是本机文件夹路径，不是网址")
	}
	if runtime.GOOS != "windows" && (strings.HasPrefix(raw, `\\`) || len(raw) > 1 && raw[1] == ':') {
		return "", errors.New("当前系统不支持 Windows 盘符或 UNC 路径")
	}
	return filepath.Clean(raw), nil
}

func checkOutputDirectory(directory string) error {
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("无法创建下载目录: %w", err)
	}
	file, err := os.CreateTemp(directory, ".juku-write-check-*")
	if err != nil {
		return fmt.Errorf("下载目录不可写: %w", err)
	}
	name := file.Name()
	closeErr := file.Close()
	removeErr := os.Remove(name)
	if closeErr != nil {
		return closeErr
	}
	return removeErr
}

func sameDirectory(first, second string) bool {
	firstAbsolute, firstErr := filepath.Abs(first)
	secondAbsolute, secondErr := filepath.Abs(second)
	if firstErr != nil || secondErr != nil {
		return first == second
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(firstAbsolute, secondAbsolute)
	}
	return firstAbsolute == secondAbsolute
}

func (cfg Config) runtimeSettingsDirectory() string {
	return cfg.dataDirectory()
}

func (cfg Config) dataDirectory() string {
	return firstNonEmpty(cfg.dataDir, "data")
}

func flagWasSet(name string) bool {
	found := false
	flag.Visit(func(option *flag.Flag) {
		if option.Name == name {
			found = true
		}
	})
	return found
}

func isProjectDirectory(directory string) bool {
	body, err := os.ReadFile(filepath.Join(directory, "go.mod"))
	fields := strings.Fields(string(body))
	return err == nil && len(fields) >= 2 && fields[0] == "module" && fields[1] == "juku"
}

func preparePortableWorkingDirectory() error {
	if isProjectDirectory(".") {
		return nil
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	directory := filepath.Dir(executable)
	if filepath.Base(directory) == "dist" && isProjectDirectory(filepath.Dir(directory)) {
		directory = filepath.Dir(directory)
	}
	if strings.HasPrefix(filepath.Base(filepath.Dir(directory)), "go-build") || strings.HasPrefix(filepath.Base(directory), "go-build") {
		return nil
	}
	return os.Chdir(directory)
}
