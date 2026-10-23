package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf16"
)

const macDirectoryPickerScript = `ObjC.import('AppKit');
function run(arguments) {
  var application = $.NSApplication.sharedApplication;
  application.setActivationPolicy($.NSApplicationActivationPolicyAccessory);
  var panel = $.NSOpenPanel.openPanel;
  panel.setTitle('选择下载文件夹');
  panel.setPrompt('选择');
  panel.setCanChooseFiles(false);
  panel.setCanChooseDirectories(true);
  panel.setAllowsMultipleSelection(false);
  panel.setCanCreateDirectories(true);
  panel.setDirectoryURL($.NSURL.fileURLWithPath(arguments[0]));
  application.activateIgnoringOtherApps(true);
  return panel.runModal === $.NSModalResponseOK ? ObjC.unwrap(panel.URL.path) : '';
}`

const windowsDirectoryPickerScript = `$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
Add-Type -AssemblyName System.Windows.Forms
[System.Windows.Forms.Application]::EnableVisualStyles()
$dialog = New-Object System.Windows.Forms.FolderBrowserDialog
$owner = New-Object System.Windows.Forms.Form
try {
  $dialog.Description = '选择下载文件夹'
  $dialog.ShowNewFolderButton = $true
  $dialog.SelectedPath = $env:JUKU_PICKER_START_DIRECTORY
  $owner.ShowInTaskbar = $false
  $owner.TopMost = $true
  $owner.Opacity = 0
  $owner.Show()
  $owner.Activate()
  if ($dialog.ShowDialog($owner) -eq [System.Windows.Forms.DialogResult]::OK) {
    [Console]::Write($dialog.SelectedPath)
  }
} finally {
  $dialog.Dispose()
  $owner.Dispose()
}`

var configureDirectoryPickerProcess = func(command *exec.Cmd) {}

func directoryPickerCommand(ctx context.Context, system, initial string) (*exec.Cmd, bool, error) {
	var command *exec.Cmd
	cancelExit := false
	switch system {
	case "darwin":
		command = exec.CommandContext(ctx, "osascript", "-l", "JavaScript", "-e", macDirectoryPickerScript, initial)
	case "windows":
		encoded := utf16.Encode([]rune(windowsDirectoryPickerScript))
		body := make([]byte, len(encoded)*2)
		for index, value := range encoded {
			body[index*2], body[index*2+1] = byte(value), byte(value>>8)
		}
		command = exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-STA", "-EncodedCommand", base64.StdEncoding.EncodeToString(body))
		command.Env = append(os.Environ(), "JUKU_PICKER_START_DIRECTORY="+initial)
	case "linux":
		if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			return nil, false, errors.New("当前没有桌面会话，请手动输入下载路径")
		}
		if executable, err := exec.LookPath("zenity"); err == nil {
			command = exec.CommandContext(ctx, executable, "--file-selection", "--directory", "--title=选择下载文件夹", "--filename="+initial+string(os.PathSeparator))
		} else if executable, err := exec.LookPath("kdialog"); err == nil {
			command = exec.CommandContext(ctx, executable, "--getexistingdirectory", initial, "--title", "选择下载文件夹")
		} else {
			return nil, false, errors.New("当前桌面没有 zenity 或 kdialog，请安装其一或手动输入下载路径")
		}
		cancelExit = true
	default:
		return nil, false, errors.New("当前系统暂不支持原生文件夹选择，请手动输入下载路径")
	}
	configureDirectoryPickerProcess(command)
	return command, cancelExit, nil
}

func nativeDirectoryPicker(ctx context.Context, initial string) (string, error) {
	command, cancelExit, err := directoryPickerCommand(ctx, runtime.GOOS, initial)
	if err != nil {
		return "", err
	}
	output, err := command.Output()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		var exitError *exec.ExitError
		if cancelExit && errors.As(err, &exitError) && exitError.ExitCode() == 1 && strings.TrimSpace(string(exitError.Stderr)) == "" {
			return "", nil
		}
		return "", fmt.Errorf("系统文件夹选择框启动失败，请确认桌面会话可用，或手动输入路径: %w", err)
	}
	return strings.TrimRight(strings.TrimPrefix(string(output), "\ufeff"), "\r\n"), nil
}

func pickerInitialDirectory(initial, fallback string) string {
	for _, candidate := range []string{initial, fallback, "."} {
		if candidate == "" {
			continue
		}
		absolute, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		for {
			if info, err := os.Stat(absolute); err == nil && info.IsDir() {
				return absolute
			}
			parent := filepath.Dir(absolute)
			if parent == absolute {
				break
			}
			absolute = parent
		}
	}
	return ""
}

func directorySelectionPath(absolute string) string {
	if working, err := os.Getwd(); err == nil {
		if relative, err := filepath.Rel(working, absolute); err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative) {
			return relative
		}
	}
	return absolute
}

func localDirectoryPickerRequest(request *http.Request) bool {
	remote, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil || !net.ParseIP(remote).IsLoopback() {
		return false
	}
	host := (&url.URL{Host: request.Host}).Hostname()
	if !strings.EqualFold(host, "localhost") && !net.ParseIP(host).IsLoopback() {
		return false
	}
	if site := request.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return false
	}
	if origin := request.Header.Get("Origin"); origin != "" {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.User != nil || parsed.Scheme != "http" && parsed.Scheme != "https" || !strings.EqualFold(parsed.Host, request.Host) {
			return false
		}
	}
	return true
}

func (a *UIApp) handleDirectoryPicker(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !localDirectoryPickerRequest(request) {
		writeJSON(writer, http.StatusForbidden, map[string]string{"error": "请在运行程序的电脑上使用 localhost 或 127.0.0.1 打开界面；远程访问请手动输入服务器路径"})
		return
	}
	contentType, _, _ := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if contentType != "application/json" {
		writeJSON(writer, http.StatusUnsupportedMediaType, map[string]string{"error": "请求必须使用 JSON"})
		return
	}
	var selection struct {
		InitialPath string `json:"initialPath"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 16384))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&selection); err != nil || len(selection.InitialPath) > 8192 || strings.ContainsAny(selection.InitialPath, "\x00\r\n") {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "无效的初始目录"})
		return
	}
	if !a.directoryPickerMu.TryLock() {
		writeJSON(writer, http.StatusConflict, map[string]string{"error": "系统文件夹选择框已打开，请先完成或取消选择"})
		return
	}
	defer a.directoryPickerMu.Unlock()
	a.mu.Lock()
	output, picker := a.cfg.OutputDir, a.directoryPicker
	a.mu.Unlock()
	if picker == nil {
		picker = nativeDirectoryPicker
	}
	ctx, cancel := context.WithTimeout(request.Context(), 5*time.Minute)
	defer cancel()
	path, err := picker(ctx, pickerInitialDirectory(selection.InitialPath, output))
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			writeJSON(writer, http.StatusGatewayTimeout, map[string]string{"error": "文件夹选择超时，请重试或手动输入路径"})
		} else {
			writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": a.redactError(err)})
		}
		return
	}
	if path == "" {
		writeJSON(writer, http.StatusOK, map[string]bool{"canceled": true})
		return
	}
	info, err := os.Stat(path)
	if err != nil || !filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\r\n") || !info.IsDir() {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "系统未返回有效文件夹，请重新选择"})
		return
	}
	path = filepath.Clean(path)
	writeJSON(writer, http.StatusOK, map[string]any{"path": path, "selectionPath": directorySelectionPath(path), "canceled": false})
}
