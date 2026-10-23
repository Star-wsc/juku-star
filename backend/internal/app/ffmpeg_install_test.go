package app

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("JUKU_TEST_FFMPEG_HELPER") == "1" {
		ffmpegTestHelper()
		return
	}
	os.Exit(m.Run())
}

func ffmpegTestHelper() {
	mode, _ := os.ReadFile(os.Args[0] + ".mode")
	if log, err := os.OpenFile(os.Args[0]+".calls", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600); err == nil {
		fmt.Fprintln(log, strings.Join(os.Args[1:], " "))
		_ = log.Close()
	}
	if string(mode) == "hang" {
		time.Sleep(time.Minute)
	}
	if string(mode) == "slow" {
		time.Sleep(100 * time.Millisecond)
	}
	if strings.Contains(strings.Join(os.Args[1:], " "), "-encoders") {
		fmt.Println(" A..... aac AAC")
		if string(mode) != "missing" {
			fmt.Println(" V..... libx264 H.264")
		}
		return
	}
	if string(mode) == "preset" {
		fmt.Fprintln(os.Stderr, "Unrecognized option 'preset'.\r\nError splitting the argument list: Option not found")
		os.Exit(8)
	}
	fmt.Print("synthetic MP4 output")
}

func testTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "juku-ffmpeg-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// Windows can keep the just-executed helper image locked briefly;
		// retry, then leave the directory rather than failing the test.
		var last error
		for i := 0; i < 20; i++ {
			last = os.RemoveAll(dir)
			if last == nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Logf("temp dir cleanup incomplete: %v", last)
	})
	return dir
}

func ffmpegTestEnvironment(t *testing.T) string {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := testTempDir(t)
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
	t.Setenv("PATH", "")
	t.Setenv("JUKU_TEST_FFMPEG_HELPER", "1")
	return root
}

func ffmpegTestProgram(t *testing.T, directory, mode string) string {
	t.Helper()
	if err := os.MkdirAll(directory, 0755); err != nil {
		t.Fatal(err)
	}
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, ffmpegExecutableName())
	if err := os.Link(program, path); err != nil {
		body, err := os.ReadFile(program)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, body, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(path+".mode", []byte(mode), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func ffmpegTestInstaller(t *testing.T, configured string) *ffmpegInstaller {
	t.Helper()
	return &ffmpegInstaller{
		configured: configured, name: ffmpegExecutableName(), directory: ffmpegManagedDirectory(),
		client: &http.Client{Transport: rankingTransport(func(*http.Request) (*http.Response, error) {
			t.Error("unexpected FFmpeg download")
			return nil, errors.New("network forbidden")
		})},
	}
}

func TestFFmpegPATHPrecedesPortableAndKeepsAutomaticSetting(t *testing.T) {
	root := ffmpegTestEnvironment(t)
	ffmpegTestProgram(t, filepath.Join(root, "bin"), "preset")
	ffmpegTestProgram(t, root, "preset")
	full := ffmpegTestProgram(t, filepath.Join(root, "系统 PATH with spaces"), "good")
	t.Setenv("PATH", filepath.Dir(full))
	d := NewDownloader(Config{FFmpeg: "ffmpeg", OutputDir: filepath.Join(root, "out"), dataDir: filepath.Join(root, "data"), settingsLoaded: true})
	if d.cfg.FFmpeg != "ffmpeg" {
		t.Fatal("automatic discovery was frozen as an explicit portable path", d.cfg.FFmpeg)
	}
	if d.ffmpegInstallation().snapshot().Status == "ready" {
		t.Fatal("unverified executable advertised as ready")
	}
	path, err := d.ensureFFmpeg(context.Background())
	if err != nil || path != full {
		t.Fatal("PATH must take precedence over current-directory and bundled programs", path, err)
	}
	if !strings.Contains(d.ffmpegInstallation().snapshot().Detail, full) {
		t.Fatal("the selected executable must be visible in status")
	}
}

func TestFFmpegSkipsIncompatiblePATHCandidates(t *testing.T) {
	root := ffmpegTestEnvironment(t)
	first := ffmpegTestProgram(t, filepath.Join(root, "minimal"), "missing")
	second := ffmpegTestProgram(t, filepath.Join(root, "advertised-only"), "preset")
	full := ffmpegTestProgram(t, filepath.Join(root, "full"), "good")
	t.Setenv("PATH", strings.Join([]string{filepath.Dir(first), filepath.Dir(second), filepath.Dir(first), filepath.Dir(full)}, string(os.PathListSeparator)))
	installer := ffmpegTestInstaller(t, "ffmpeg")
	var events []diagnosticEvent
	installer.record = func(event diagnosticEvent) { events = append(events, event) }
	path, err := installer.ensure(context.Background())
	if err != nil || path != full {
		t.Fatal("a PATH hit without working encoders must not mask a usable fallback", path, err)
	}
	if len(events) != 3 || events[0].Event != "ffmpeg.rejected" || !strings.Contains(events[1].Message, "preset") || events[2].Event != "ffmpeg.ready" || !strings.Contains(events[2].Message, full) {
		t.Fatal("selection diagnostics must explain rejected candidates and the final path", events)
	}
	if candidates := ffmpegCandidates("ffmpeg", installer.directory); len(candidates) != 3 {
		t.Fatal("duplicate PATH entries were not deduplicated", candidates)
	}
}

func TestFFmpegExplicitPathsRemainAuthoritative(t *testing.T) {
	root := ffmpegTestEnvironment(t)
	full := ffmpegTestProgram(t, filepath.Join(root, "full"), "good")
	bad := ffmpegTestProgram(t, filepath.Join(root, "explicit"), "preset")
	t.Setenv("PATH", filepath.Dir(full))
	installer := ffmpegTestInstaller(t, bad)
	path, err := installer.ensure(context.Background())
	if err == nil || path != "" || !strings.Contains(err.Error(), "preset") || !strings.Contains(err.Error(), bad) {
		t.Fatal("an invalid explicit path must report its failure without switching programs", path, err)
	}
	if installer.snapshot().Status != "failed" {
		t.Fatal("invalid explicit program was marked ready")
	}
	if err := os.WriteFile(bad+".mode", []byte("good"), 0600); err != nil {
		t.Fatal(err)
	}
	installer.retry()
	path, err = installer.ensure(context.Background())
	if err != nil || path != bad {
		t.Fatal("retry must probe a corrected explicit program", path, err)
	}
	installer = ffmpegTestInstaller(t, " \""+full+"\" ")
	path, err = installer.ensure(context.Background())
	if err != nil || path != full {
		t.Fatal("quoted paths with spaces were not resolved", path, err)
	}
	installer = ffmpegTestInstaller(t, filepath.Join(root, "missing", "ffmpeg.exe"))
	if _, err := installer.ensure(context.Background()); err == nil || !strings.Contains(err.Error(), "找不到指定") {
		t.Fatal("missing explicit path must not silently download", err)
	}
}

func TestFFmpegSharedProbeIsCachedAndCancelable(t *testing.T) {
	root := ffmpegTestEnvironment(t)
	program := ffmpegTestProgram(t, filepath.Join(root, "slow"), "slow")
	installer := ffmpegTestInstaller(t, program)
	var wait sync.WaitGroup
	for index := 0; index < 8; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if path, err := installer.ensure(context.Background()); err != nil || path != program {
				t.Error(path, err)
			}
		}()
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := installer.ensure(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled waiter did not exit", err)
	}
	wait.Wait()
	if _, err := installer.ensure(context.Background()); err != nil {
		t.Fatal(err)
	}
	log, err := os.ReadFile(program + ".calls")
	if err != nil || len(strings.Split(strings.TrimSpace(string(log)), "\n")) != 2 {
		t.Fatal("concurrent users must share one encoder query and one real probe", string(log), err)
	}
	hanging := ffmpegTestProgram(t, filepath.Join(root, "hang"), "hang")
	ctx, cancel = context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := validateFFmpeg(ctx, hanging); err == nil || time.Since(start) > 3*time.Second {
		t.Fatal("capability probe must honor cancellation", err, time.Since(start))
	}
}

func ffmpegTestPackage(t *testing.T, installer *ffmpegInstaller) *atomic.Int32 {
	t.Helper()
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(program)
	if err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	writer, _ := gzip.NewWriterLevel(&archive, gzip.BestSpeed)
	_, _ = writer.Write(binary)
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	digest := func(body []byte) string { value := sha256.Sum256(body); return hex.EncodeToString(value[:]) }
	document := []byte("synthetic test package documentation")
	installer.pack = ffmpegPackage{platform: "fixture", archiveSize: int64(archive.Len()), binarySize: int64(len(binary)), archiveSHA256: digest(archive.Bytes()), binarySHA256: digest(binary), licenseSHA256: digest(document), readmeSHA256: digest(document)}
	var requests atomic.Int32
	installer.client = &http.Client{Transport: rankingTransport(func(request *http.Request) (*http.Response, error) {
		requests.Add(1)
		body := document
		if strings.HasSuffix(request.URL.Path, ".gz") {
			body = archive.Bytes()
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Request: request}, nil
	})}
	return &requests
}

func TestFFmpegAutomaticFallbackPreservesExistingPortableProgram(t *testing.T) {
	root := ffmpegTestEnvironment(t)
	old := ffmpegTestProgram(t, filepath.Join(root, "bin", runtime.GOOS+"-"+runtime.GOARCH), "missing")
	info, err := os.Stat(old)
	if err != nil {
		t.Fatal(err)
	}
	installer := ffmpegTestInstaller(t, "ffmpeg")
	requests := ffmpegTestPackage(t, installer)
	path, err := installer.ensure(context.Background())
	if err != nil || path != filepath.Join(installer.directory, installer.name) || path == old {
		t.Fatal("verified auto-download must be selected after rejecting the old portable program", path, err)
	}
	if requests.Load() != 3 {
		t.Fatal("archive, license and build details must all be fetched", requests.Load())
	}
	after, err := os.Stat(old)
	if err != nil || !os.SameFile(info, after) {
		t.Fatal("existing portable program was replaced", err)
	}
	restarted := ffmpegTestInstaller(t, "ffmpeg")
	if next, err := restarted.ensure(context.Background()); err != nil || next != path {
		t.Fatal("restart must reuse the verified installation without downloading", next, err)
	}
}

func TestFFmpegInstallationRejectsCorruptionAndInvalidDestination(t *testing.T) {
	root := ffmpegTestEnvironment(t)
	installer := ffmpegTestInstaller(t, "ffmpeg")
	ffmpegTestPackage(t, installer)
	installer.pack.archiveSHA256 = strings.Repeat("0", 64)
	if _, err := installer.install(context.Background()); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatal("corrupt download was accepted", err)
	}
	if _, err := os.Stat(installer.directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a corrupt download must not create a usable installation", err)
	}
	ffmpegTestPackage(t, installer)
	bad := ffmpegTestProgram(t, installer.directory, "preset")
	if path, err := installer.install(context.Background()); err == nil || path != "" {
		t.Fatal("an existing invalid destination must not turn an install failure into success", path, err)
	}
	if mode, err := os.ReadFile(bad + ".mode"); err != nil || string(mode) != "preset" {
		t.Fatal("existing installation contents were changed", root, err)
	}
}

func TestFFmpegLocalPlaybackProbe(t *testing.T) {
	program, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("local FFmpeg unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	if err := validateFFmpeg(ctx, program); err != nil {
		t.Fatal(err)
	}
}
