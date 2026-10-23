package app

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type mergeMediaInfo struct {
	signature string
	width     int
	height    int
	audio     bool
	duration  time.Duration
}

func probeMergeMedia(ctx context.Context, ffmpeg, path string) (mergeMediaInfo, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	command := exec.CommandContext(probeCtx, ffmpeg, "-hide_banner", "-nostdin", "-loglevel", "info",
		"-protocol_whitelist", "file,pipe", "-i", path, "-map", "0:v:0", "-map", "0:a:0?",
		"-c", "copy", "-t", "0", "-f", "framehash", "-hash", "sha256", "pipe:1")
	output := &cappedStringWriter{limit: 64 * 1024}
	diagnostics := &cappedStringWriter{limit: 64 * 1024}
	command.Stdout, command.Stderr = output, diagnostics
	if err := command.Run(); err != nil {
		if probeCtx.Err() != nil {
			return mergeMediaInfo{}, probeCtx.Err()
		}
		return mergeMediaInfo{}, fmt.Errorf("检测 %s 失败：%w %s", filepath.Base(path), err, truncate(diagnostics.String(), 1000))
	}
	var info mergeMediaInfo
	var signature []string
	for _, line := range strings.Split(output.String(), "\n") {
		line = strings.TrimSpace(line)
		for _, prefix := range []string{"#extradata ", "#tb ", "#media_type ", "#codec_id ", "#dimensions ", "#sar ", "#sample_rate ", "#channel_layout"} {
			if strings.HasPrefix(line, prefix) {
				signature = append(signature, line)
				break
			}
		}
		if strings.HasPrefix(line, "#dimensions 0:") {
			_, _ = fmt.Sscanf(strings.TrimSpace(strings.TrimPrefix(line, "#dimensions 0:")), "%dx%d", &info.width, &info.height)
		}
		if line == "#media_type 1: audio" {
			info.audio = true
		}
	}
	if match := playbackDurationPattern.FindStringSubmatch(diagnostics.String()); len(match) == 4 {
		hours, _ := strconv.ParseFloat(match[1], 64)
		minutes, _ := strconv.ParseFloat(match[2], 64)
		seconds, _ := strconv.ParseFloat(match[3], 64)
		info.duration = time.Duration((hours*3600 + minutes*60 + seconds) * float64(time.Second))
	}
	if info.width <= 0 || info.height <= 0 || info.duration <= 0 {
		return mergeMediaInfo{}, fmt.Errorf("%s 缺少有效的视频尺寸或时长，无法安全合并", filepath.Base(path))
	}
	info.signature = strings.Join(signature, "\n")
	return info, nil
}

func runMergeFFmpeg(ctx context.Context, ffmpeg string, args []string, progress func(time.Duration)) error {
	command := exec.CommandContext(ctx, ffmpeg, append([]string{
		"-hide_banner", "-nostdin", "-nostats", "-loglevel", "error", "-progress", "pipe:1",
	}, args...)...)
	diagnostics := &cappedStringWriter{limit: 64 * 1024}
	command.Stderr = diagnostics
	output, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	defer output.Close()
	if err := command.Start(); err != nil {
		return err
	}
	scanner := bufio.NewScanner(output)
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), "=")
		if ok && key == "out_time_us" && progress != nil {
			if micros, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil && micros >= 0 {
				progress(time.Duration(micros) * time.Microsecond)
			}
		}
	}
	err = command.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("FFmpeg 处理失败：%w %s", err, truncate(diagnostics.String(), 1500))
	}
	return scanner.Err()
}

func normalizedMergeArgs(input, output string, info mergeMediaInfo, width, height int, audio bool) []string {
	args := []string{"-xerror", "-threads", "2", "-protocol_whitelist", "file,pipe", "-i", input}
	if audio && !info.audio {
		args = append(args, "-f", "lavfi", "-i", "anullsrc=channel_layout=stereo:sample_rate=48000")
	}
	args = append(args, "-map", "0:v:0")
	if audio {
		audioInput := "0:a:0"
		if !info.audio {
			audioInput = "1:a:0"
		}
		args = append(args, "-map", audioInput, "-c:a", "aac", "-b:a", "128k", "-ar", "48000", "-ac", "2",
			"-af", "aresample=48000:async=1:first_pts=0,apad", "-shortest")
	} else {
		args = append(args, "-an")
	}
	filter := fmt.Sprintf("setpts=PTS-STARTPTS,fps=30,scale=%d:%d:force_original_aspect_ratio=decrease:force_divisible_by=2,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,setsar=1", width, height, width, height)
	return append(args, "-sn", "-dn", "-map_metadata", "-1", "-vf", filter,
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "18", "-profile:v", "high", "-pix_fmt", "yuv420p",
		"-threads:v", "2", "-g", "60", "-sc_threshold", "0", "-video_track_timescale", "90000",
		"-max_muxing_queue_size", "4096", "-t", strconv.FormatFloat(info.duration.Seconds(), 'f', 6, 64), "-f", "mp4", "-y", output)
}

func mergeMediaFiles(ctx context.Context, ffmpeg string, paths []string, output string, report func(int, string)) (string, error) {
	if len(paths) == 0 {
		return "", fmt.Errorf("没有可合并的分集")
	}
	lastProgress, lastDetail := -1, ""
	notify := func(progress int, detail string) {
		if progress > 99 {
			progress = 99
		}
		if progress < lastProgress {
			progress = lastProgress
		}
		if report != nil && (progress != lastProgress || detail != lastDetail) {
			report(progress, detail)
		}
		lastProgress, lastDetail = progress, detail
	}
	metadata := make([]mergeMediaInfo, 0, len(paths))
	var duration time.Duration
	var width, height int
	var audio, normalize bool
	for index, path := range paths {
		notify(index*5/len(paths), fmt.Sprintf("检测分集编码 %d/%d", index+1, len(paths)))
		info, err := probeMergeMedia(ctx, ffmpeg, path)
		if err != nil {
			return "", err
		}
		if index > 0 && info.signature != metadata[0].signature {
			normalize = true
		}
		metadata = append(metadata, info)
		duration += info.duration
		if info.width > width {
			width = info.width
		}
		if info.height > height {
			height = info.height
		}
		audio = audio || info.audio
	}
	work, err := os.MkdirTemp(filepath.Dir(output), ".merge-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(work)
	inputs := append([]string(nil), paths...)
	method, start := "原编码快速合并", 5
	if normalize {
		method, start = "兼容合并（H.264 / AAC）", 95
		width += width % 2
		height += height % 2
		var processed time.Duration
		for index, path := range paths {
			detail := fmt.Sprintf("编码不一致，统一格式 %d/%d", index+1, len(paths))
			notify(5+int(processed*90/duration), detail)
			temporary := filepath.Join(work, fmt.Sprintf("%06d.mp4", index+1))
			err := runMergeFFmpeg(ctx, ffmpeg, normalizedMergeArgs(path, temporary, metadata[index], width, height, audio), func(elapsed time.Duration) {
				if elapsed > metadata[index].duration {
					elapsed = metadata[index].duration
				}
				notify(5+int((processed+elapsed)*90/duration), detail)
			})
			if err != nil {
				return "", fmt.Errorf("转换 %s 失败：%w；原分集已保留", filepath.Base(path), err)
			}
			processed += metadata[index].duration
			inputs[index] = temporary
		}
	}
	var manifest strings.Builder
	for _, path := range inputs {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&manifest, "file '%s'\n", strings.ReplaceAll(filepath.ToSlash(absolute), "'", "'\\''"))
	}
	list := filepath.Join(work, "inputs.ffconcat")
	if err := os.WriteFile(list, []byte(manifest.String()), 0600); err != nil {
		return "", err
	}
	notify(start, method)
	err = runMergeFFmpeg(ctx, ffmpeg, []string{"-protocol_whitelist", "file,pipe", "-f", "concat", "-safe", "0", "-i", list,
		"-map", "0:v:0", "-map", "0:a:0?", "-sn", "-dn", "-c", "copy", "-movflags", "+faststart", "-y", output}, func(elapsed time.Duration) {
		notify(start+int(elapsed*time.Duration(99-start)/duration), method)
	})
	if err != nil {
		return "", err
	}
	notify(99, "核对合并文件时长")
	merged, err := probeMergeMedia(ctx, ffmpeg, output)
	if err != nil {
		return "", err
	}
	tolerance := duration / 100
	if tolerance < 2*time.Second {
		tolerance = 2 * time.Second
	}
	if merged.duration < duration-tolerance || merged.duration > duration+tolerance {
		return "", fmt.Errorf("合并文件时长不符（预计 %.1f 秒，实际 %.1f 秒），原分集已保留", duration.Seconds(), merged.duration.Seconds())
	}
	return method, nil
}
