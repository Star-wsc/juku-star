package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const defaultUIListenAddress = "0.0.0.0:8998"

type boolOverride struct {
	set bool
	val bool
}

func (b *boolOverride) String() string { return strconv.FormatBool(b.val) }

func (b *boolOverride) Set(s string) error {
	v, err := strconv.ParseBool(s)
	if err != nil {
		return err
	}
	b.set = true
	b.val = v
	return nil
}

func openBrowser(rawURL string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", rawURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		cmd = exec.Command("xdg-open", rawURL)
	}
	if err := cmd.Start(); err != nil {
		fmt.Printf("  自动打开浏览器失败，请手动访问：%s (%v)\n", rawURL, err)
	}
}

func normalizeListen(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return defaultUIListenAddress
	}
	if strings.HasPrefix(addr, ":") {
		return "0.0.0.0" + addr
	}
	return addr
}

func publicURL(addr string) string {
	if strings.HasPrefix(addr, "127.0.0.1:") || strings.HasPrefix(addr, "localhost:") {
		return "http://" + addr
	}
	if strings.HasPrefix(addr, "0.0.0.0:") || strings.HasPrefix(addr, "[::]:") {
		if _, port, ok := strings.Cut(addr, ":"); ok {
			return "http://127.0.0.1:" + port
		}
	}
	return "http://" + addr
}

func usage() {
	fmt.Print(`短剧库 - 在线追剧与下载管理

请仅下载你拥有权利或已获授权的视频内容。

用法：
  直接执行 go run . 或编译后的程序，默认启动浏览器管理界面

常用参数：
  -ui=true|false               启动/关闭本地浏览器管理界面，默认 true
  -listen 地址                UI 监听地址，默认 0.0.0.0:8998，支持局域网访问
  -open=true|false            UI 模式是否自动打开浏览器，默认 true
  -admin-user 用户名           管理员账号，首次默认 admin
  -admin-password 密码         指定管理员密码；首次未指定时随机生成并输出，首次登录须修改
  -mode all|search|id|list    CLI 模式，需配合 -ui=false，默认 all
  -keyword 关键词             search 模式关键词
  -id 剧ID                    id 模式下载单部剧
  -out 目录                   自定义下载目录，默认 ./短剧下载，支持相对或绝对路径
  -c 并发数                   下载并发，默认 2，可在界面设置中修改
  -pages 页数                 每频道扫描页数；红果为 App 每批页数，默认 20
  -data-dir 目录              配置、缓存和任务目录，默认 ./data
  -config 文件                首次运行时导入旧版配置（可选）
  -ffmpeg 路径                ffmpeg 路径，默认从 PATH 查找
  -refresh                   CLI 模式强制更新剧库，默认读取本地缓存
  -more                      CLI 模式从保存的红果 App 分页位置继续加载
  -insecure-tls=true|false    是否跳过 TLS 证书校验，默认 false

示例：
  juku
  juku -open=false
  juku -ui=false -mode all
  juku -ui=false -mode search -keyword 女总裁
  juku -ui=false -mode id -id 6a928xxxxx
  juku -ui=false -mode list
`)
}

func Run() {
	mode := flag.String("mode", "all", "all/search/id/list")
	keyword := flag.String("keyword", "", "search keyword")
	id := flag.String("id", "", "drama id")
	out := flag.String("out", "", "output directory")
	concurrency := flag.Int("c", 0, "download concurrency")
	pages := flag.Int("pages", 0, "max pages per sort")
	configFile := flag.String("config", "", "import a legacy config on first startup")
	dataDir := flag.String("data-dir", "data", "configuration, cache and task directory")
	ffmpeg := flag.String("ffmpeg", "", "ffmpeg path")
	adminUser := flag.String("admin-user", "", "administrator username, default admin on first startup")
	adminPassword := flag.String("admin-password", "", "administrator password; default random on first startup")
	uiMode := flag.Bool("ui", true, "start local web UI")
	listen := flag.String("listen", defaultUIListenAddress, "UI listen address; use 127.0.0.1:8998 for local-only access")
	open := flag.Bool("open", true, "open browser automatically in UI mode")
	refresh := flag.Bool("refresh", false, "refresh the CLI library instead of using local cache")
	more := flag.Bool("more", false, "continue loading the Hongguo App catalog from its saved cursor")
	var insecureTLS boolOverride
	flag.Var(&insecureTLS, "insecure-tls", "skip TLS certificate verification")
	help := flag.Bool("help", false, "show help")
	flag.Parse()
	if *help {
		usage()
		return
	}
	if *refresh && *more {
		fmt.Fprintln(os.Stderr, "-refresh 和 -more 不能同时使用")
		os.Exit(2)
	}

	if !flagWasSet("data-dir") && !flagWasSet("config") {
		if err := preparePortableWorkingDirectory(); err != nil {
			fmt.Fprintln(os.Stderr, "初始化程序目录失败:", err)
			os.Exit(1)
		}
	}
	cfg, configErr := loadApplicationConfig(*dataDir, *configFile, *out)
	if configErr != nil {
		fmt.Fprintln(os.Stderr, "初始化配置失败:", publicError(configErr))
		os.Exit(2)
	}
	cfg.adminUsername = firstNonEmpty(*adminUser, os.Getenv("JUKU_ADMIN_USER"), "admin")
	cfg.adminUserExplicit = flagWasSet("admin-user") || os.Getenv("JUKU_ADMIN_USER") != ""
	cfg.adminPassword = os.Getenv("JUKU_ADMIN_PASSWORD")
	if flagWasSet("admin-password") {
		cfg.adminPassword = *adminPassword
	}
	cfg.adminPasswordExplicit = flagWasSet("admin-password") || os.Getenv("JUKU_ADMIN_PASSWORD") != ""
	if *concurrency > 0 {
		cfg.Concurrency = *concurrency
	}
	if *pages > 0 {
		cfg.MaxPagesPerSort = *pages
	}
	if *ffmpeg != "" {
		cfg.FFmpeg = *ffmpeg
	}
	if insecureTLS.set {
		cfg.InsecureTLS = insecureTLS.val
	}
	if err := cfg.validate(); err != nil {
		fmt.Fprintf(os.Stderr, "配置错误: %v\n", err)
		os.Exit(2)
	}
	d := NewDownloader(cfg)
	cfg = d.cfg
	if err := os.MkdirAll(cfg.OutputDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "创建输出目录失败: %v\n", err)
		os.Exit(1)
	}
	_, ffmpegErr := exec.LookPath(portableFFmpeg(cfg.FFmpeg))
	fmt.Printf("输出目录: %s\n下载并发: %d\nFFmpeg 已找到（后台校验）: %t\nTLS证书校验: %t\n", cfg.OutputDir, cfg.Concurrency, ffmpegErr == nil, !cfg.InsecureTLS)
	d.recordDiagnostic(diagnosticEvent{Level: "info", Event: "app.started", Message: "短剧库已启动"})
	fmt.Printf("诊断日志: %s\n", d.diagnostics.path)
	if ffmpegErr != nil {
		if automaticFFmpeg(cfg.FFmpeg) {
			fmt.Println("未找到 FFmpeg，将自动下载匹配当前系统的便携版本；如下载失败，请在界面中调整代理后重试。")
		} else {
			fmt.Println("指定的 FFmpeg 路径未找到，请更正 download.ffmpeg 或 -ffmpeg。")
		}
	}

	if *uiMode {
		addr := normalizeListen(*listen)
		app := NewUIApp(d)
		pageURL := publicURL(addr)
		fmt.Printf("监听地址: %s | 管理界面已启动: %s\n", addr, pageURL)
		if *open {
			go func() {
				time.Sleep(500 * time.Millisecond)
				openBrowser(pageURL)
			}()
		}
		if err := app.ListenAndServe(addr); err != nil && !errors.Is(err, http.ErrServerClosed) {
			d.recordDiagnostic(diagnosticEvent{Event: "app.failed", Message: err.Error()})
			fmt.Fprintf(os.Stderr, "UI 服务停止: %v\n", err)
			os.Exit(1)
		}
		return
	}

	ctx := context.Background()
	var dramas []Drama
	var err error
	switch strings.ToLower(*mode) {
	case "id":
		if strings.TrimSpace(*id) == "" {
			fmt.Fprintln(os.Stderr, "-mode id 需要 -id")
			os.Exit(2)
		}
		dramas = []Drama{{ID: strings.TrimSpace(*id), Title: "短剧"}}
	case "all", "search", "list":
		if *more {
			dramas, err = d.RefreshDramas(context.WithValue(ctx, libraryMoreKey{}, true), sourceHongguo)
		} else if *refresh {
			dramas, err = d.RefreshDramas(ctx, "")
		} else {
			dramas, err = d.GetAllDramas(ctx)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "  剧库部分来源失败，继续处理已获取的 %d 部：%v\n", len(dramas), err)
			if len(dramas) == 0 {
				os.Exit(1)
			}
		}
		if strings.ToLower(*mode) == "search" {
			kw := strings.TrimSpace(*keyword)
			if kw == "" {
				fmt.Fprintln(os.Stderr, "-mode search 需要 -keyword")
				os.Exit(2)
			}
			var filtered []Drama
			for _, dr := range dramas {
				text := dr.DisplayTitle() + " " + dr.Desc + " " + dr.Intro + " " + dr.ChannelName
				if strings.Contains(strings.ToLower(text), strings.ToLower(kw)) {
					filtered = append(filtered, dr)
				}
			}
			dramas = filtered
			fmt.Printf(" 搜索命中：%d 部\n", len(dramas))
		}
		if strings.ToLower(*mode) == "list" {
			for i, dr := range dramas {
				fmt.Printf("%04d  [%s] %s  %s\n", i+1, dr.ChannelName, dr.ID, dr.DisplayTitle())
			}
			return
		}
	default:
		usage()
		os.Exit(2)
	}
	if len(dramas) == 0 {
		fmt.Println("没有待下载短剧。")
		return
	}

	var tasks []Task
	for i, dr := range dramas {
		fmt.Printf(" [%d/%d] 获取章节：《%s》 %s\n", i+1, len(dramas), dr.DisplayTitle(), dr.ID)
		dramaTasks, err := d.BuildDramaTasks(ctx, dr)
		if err != nil {
			fmt.Printf("  获取章节失败：《%s》 %v\n", dr.DisplayTitle(), err)
			continue
		}
		tasks = append(tasks, dramaTasks...)
	}
	fmt.Printf(" 待处理视频：%d 集\n", len(tasks))
	results := DownloadTasks(ctx, d, tasks)
	var ok, fail int
	for _, r := range results {
		if r.OK {
			ok++
		} else {
			fail++
		}
	}
	if err := saveFailures(cfg.dataDirectory(), results); err != nil {
		fmt.Printf("  写入 failed.json 失败: %v\n", err)
	}
	fmt.Printf("\n 完成：成功/跳过 %d，失败 %d\n", ok, fail)
	if fail > 0 {
		fmt.Printf("失败详情已写入：%s\n", filepath.Join(cfg.dataDirectory(), "failed.json"))
		os.Exit(1)
	}
}
