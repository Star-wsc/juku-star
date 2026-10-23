package app

import (
	"math"
	"os"
	"strings"
	"sync"
	"time"
)

type cappedStringWriter struct {
	b     strings.Builder
	limit int
}

func (w *cappedStringWriter) Write(p []byte) (int, error) {
	if w.limit <= 0 {
		return len(p), nil
	}
	remaining := w.limit - w.b.Len()
	if remaining > 0 {
		if len(p) > remaining {
			_, _ = w.b.Write(p[:remaining])
		} else {
			_, _ = w.b.Write(p)
		}
	}
	return len(p), nil
}

func (w *cappedStringWriter) String() string { return w.b.String() }

type downloadProgressState struct {
	mu          sync.Mutex
	callbackMu  sync.Mutex
	callback    func(DownloadProgress)
	partPath    string
	outPath     string
	totalBytes  int64
	mediaTotal  time.Duration
	mediaDone   time.Duration
	started     time.Time
	lastSample  time.Time
	lastBytes   int64
	speed       float64
	lastReport  time.Time
	lastPercent int
	lastPhase   string
}

func newDownloadProgressState(partPath, outPath string, totalBytes int64, callback func(DownloadProgress)) *downloadProgressState {
	return &downloadProgressState{partPath: partPath, outPath: outPath, totalBytes: totalBytes, callback: callback, started: time.Now()}
}

func (p *downloadProgressState) setMediaTotal(total time.Duration) {
	p.mu.Lock()
	p.mediaTotal = total
	p.mu.Unlock()
}

func (p *downloadProgressState) setMediaElapsed(elapsed time.Duration) {
	if elapsed < 0 {
		elapsed = 0
	}
	p.mu.Lock()
	if elapsed > p.mediaDone {
		p.mediaDone = elapsed
	}
	p.mu.Unlock()
}

func (p *downloadProgressState) snapshot(now time.Time, phase string) DownloadProgress {
	p.mu.Lock()
	defer p.mu.Unlock()
	bytesDownloaded := int64(0)
	if st, err := os.Stat(p.partPath); err == nil && !st.IsDir() {
		bytesDownloaded = st.Size()
	} else if phase == "completed" && p.outPath != "" {
		if st, err := os.Stat(p.outPath); err == nil && !st.IsDir() {
			bytesDownloaded = st.Size()
		}
	}
	if !p.lastSample.IsZero() {
		seconds := now.Sub(p.lastSample).Seconds()
		if seconds > 0 && bytesDownloaded >= p.lastBytes {
			p.speed = float64(bytesDownloaded-p.lastBytes) / seconds
		}
	}
	p.lastSample = now
	p.lastBytes = bytesDownloaded
	if p.speed < 0 || math.IsNaN(p.speed) || math.IsInf(p.speed, 0) {
		p.speed = 0
	}
	elapsed := now.Sub(p.started)
	if elapsed < 0 {
		elapsed = 0
	}
	mediaElapsed := p.mediaDone
	mediaTotal := p.mediaTotal
	percent := 0
	if mediaTotal > 0 {
		percent = int(float64(mediaElapsed) / float64(mediaTotal) * 100)
	} else if p.totalBytes > 0 {
		percent = int(float64(bytesDownloaded) / float64(p.totalBytes) * 100)
	}
	if phase == "completed" {
		percent = 100
		if mediaTotal > 0 {
			mediaElapsed = mediaTotal
		}
	}
	if percent < 0 {
		percent = 0
	}
	if phase != "completed" && percent >= 100 {
		percent = 99
	} else if percent > 100 {
		percent = 100
	}
	remaining := time.Duration(0)
	if mediaTotal > 0 && mediaElapsed > 0 {
		remaining = time.Duration(float64(mediaTotal-mediaElapsed) * float64(elapsed) / float64(mediaElapsed))
	} else if p.totalBytes > 0 && p.speed > 0 && bytesDownloaded < p.totalBytes {
		remaining = time.Duration(float64(p.totalBytes-bytesDownloaded) / p.speed * float64(time.Second))
	}
	if remaining < 0 || math.IsNaN(float64(remaining)) || math.IsInf(float64(remaining), 0) {
		remaining = 0
	}
	return DownloadProgress{Percent: percent, DownloadedBytes: bytesDownloaded, TotalBytes: p.totalBytes, SpeedBytesPerSecond: p.speed, Elapsed: elapsed, Remaining: remaining, MediaElapsed: mediaElapsed, MediaTotal: mediaTotal, Phase: phase}
}

func (p *downloadProgressState) report(phase string, force bool) {
	if p.callback == nil {
		return
	}
	now := time.Now()
	p.callbackMu.Lock()
	defer p.callbackMu.Unlock()
	progress := p.snapshot(now, phase)
	p.mu.Lock()
	changed := force || progress.Percent != p.lastPercent || progress.Phase != p.lastPhase || now.Sub(p.lastReport) >= time.Second
	if changed {
		p.lastPercent = progress.Percent
		p.lastPhase = progress.Phase
		p.lastReport = now
	}
	p.mu.Unlock()
	if changed {
		p.callback(progress)
	}
}
