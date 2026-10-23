package app

import (
	"context"
	"fmt"
	"io"
	"os/exec"
)

type playbackProcess struct {
	command *exec.Cmd
	cancel  context.CancelFunc
	stdout  io.ReadCloser
	log     *playbackLog
	waited  bool
}

func startPlaybackProcess(ctx context.Context, ffmpeg string, args []string) (*playbackProcess, error) {
	ctx, cancel := context.WithCancel(ctx)
	command := ffmpegMediaCommand(ctx, ffmpeg, args...)
	stdout, err := command.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	log := &playbackLog{text: cappedStringWriter{limit: 64 * 1024}, ready: make(chan struct{})}
	command.Stderr = log
	if err := command.Start(); err != nil {
		cancel()
		_ = stdout.Close()
		return nil, fmt.Errorf("无法启动在线播放处理：%w", err)
	}
	return &playbackProcess{command: command, cancel: cancel, stdout: stdout, log: log}, nil
}

func (process *playbackProcess) Wait() error {
	process.waited = true
	return process.command.Wait()
}

func (process *playbackProcess) Close() {
	process.cancel()
	_ = process.stdout.Close()
	if !process.waited {
		_ = process.Wait()
	}
}
