package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const (
	hongguoDeviceRegisterURL   = "https://log.snssdk.com/service/2/device_register/"
	hongguoDevicePoolFile      = "hongguo-devices.json"
	hongguoDeviceRotations     = 3
	hongguoDeviceRegisterPause = 10 * time.Minute
)

type hongguoRegisterRejection struct{ error }

// hongguoAppQuery 是红果 App 的公共查询参数；设备注册时要去掉 device_id/iid。
func hongguoAppQuery(deviceID, installID string) url.Values {
	return url.Values{
		"aid": {"8662"}, "app_name": {"novelread"}, "version_code": {"73532"}, "version_name": {"7.3.5.32"},
		"manifest_version_code": {"73532"}, "update_version_code": {"73532"}, "channel": {"update_64"},
		"device_platform": {"android"}, "os": {"android"}, "ssmix": {"a"}, "device_type": {"25053RT47C"},
		"device_brand": {"Redmi"}, "language": {"zh"}, "os_api": {"36"}, "os_version": {"16"},
		"resolution": {"1280*2772"}, "dpi": {"520"}, "ac": {"wifi"}, "device_id": {deviceID}, "iid": {installID},
	}
}

func newHongguoDeviceID() string {
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return strconv.FormatUint(1_000_000_000_000_000_000+binary.BigEndian.Uint64(random[:])%8_000_000_000_000_000_000, 10)
}

func (downloader *Downloader) hongguoDevice() (string, string, bool) {
	client := downloader.hongguoClient()
	client.mu.Lock()
	defer client.mu.Unlock()
	return client.state.DeviceID, client.state.InstallID, client.state.Registered
}

// ensureHongguoDevice 确保有一个可用的已注册设备。
// rejected 是被上游判为无效的设备号：优先切换到设备池中的其它设备，池中没有时才注册新设备。
func (downloader *Downloader) ensureHongguoDevice(ctx context.Context, rejected string) error {
	client := downloader.hongguoClient()
	client.mu.Lock()
	if rejected == "" && client.state.Registered {
		client.mu.Unlock()
		return nil
	}
	if useHongguoDeviceLocked(client, rejected) {
		client.mu.Unlock()
		return nil
	}
	client.mu.Unlock()

	client.deviceMu.Lock()
	defer client.deviceMu.Unlock()
	// long: 并发请求可能已经注册好新设备，拿到锁后必须重新判断。
	client.mu.Lock()
	if rejected == "" && client.state.Registered {
		client.mu.Unlock()
		return nil
	}
	if useHongguoDeviceLocked(client, rejected) {
		client.mu.Unlock()
		return nil
	}
	client.mu.Unlock()
	if time.Now().Before(client.registerPausedUntil) {
		return fmt.Errorf("红果设备注册暂不可用：%v", client.registerPauseErr)
	}
	deviceID, installID, err := downloader.registerHongguoDevice(ctx)
	if err != nil {
		var rejection hongguoRegisterRejection
		if errors.As(err, &rejection) {
			client.registerPausedUntil, client.registerPauseErr = time.Now().Add(hongguoDeviceRegisterPause), err
		}
		return err
	}
	client.mu.Lock()
	addHongguoDeviceLocked(client, deviceID, installID)
	client.mu.Unlock()
	downloader.persistHongguoDevicePool()
	return nil
}

func (downloader *Downloader) registerHongguoDevice(ctx context.Context) (string, string, error) {
	header := map[string]any{
		"display_name": "红果免费短剧", "aid": 8662, "app_version": "7.3.5.32", "version_code": 73532,
		"manifest_version_code": 73532, "update_version_code": 73532, "channel": "update_64", "package": "com.phoenix.read",
		"app_language": "zh", "os": "Android", "os_version": "16", "os_api": 36, "device_model": "25053RT47C",
		"device_brand": "Redmi", "device_manufacturer": "Xiaomi", "cpu_abi": "arm64-v8a", "density_dpi": 520,
		"display_density": "xxhdpi", "resolution": "2772x1280", "language": "zh", "timezone": 8, "access": "wifi",
		"openudid": randomHex(8), "clientudid": uuidLike(), "cdid": uuidLike(),
		"region": "CN", "tz_name": "Asia/Shanghai", "tz_offset": 28800, "sim_region": "cn", "device_platform": "android",
		"not_request_sender": 0, "is_system_app": 0, "sdk_flavor": "china", "rom": "OS2.0", "rom_version": "BP2A.250605.031.A3",
	}
	body, err := json.Marshal(map[string]any{"magic_tag": "ss_app_log", "header": header, "_gen_time": time.Now().UnixMilli()})
	if err != nil {
		return "", "", err
	}
	query := hongguoAppQuery("", "")
	query.Del("device_id")
	query.Del("iid")
	query.Set("req_id", uuidLike())
	query.Set("_rticket", strconv.FormatInt(time.Now().UnixMilli(), 10))
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, hongguoDeviceRegisterURL+"?"+query.Encode(), bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	request.Header.Set("User-Agent", hongguoAppUserAgent)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	response, err := downloader.doCatalogRequestWithTimeout(request, 20*time.Second)
	if err != nil {
		return "", "", err
	}
	defer response.Body.Close()
	content := make([]byte, 0, 64<<10)
	buffer := make([]byte, 32<<10)
	for {
		n, readErr := response.Body.Read(buffer)
		content = append(content, buffer[:n]...)
		if len(content) > 64<<10 {
			return "", "", hongguoRegisterRejection{errors.New("红果设备注册响应超出大小限制")}
		}
		if readErr != nil {
			break
		}
	}
	if response.StatusCode != http.StatusOK {
		return "", "", hongguoRegisterRejection{fmt.Errorf("红果设备注册 HTTP %d", response.StatusCode)}
	}
	var result map[string]any
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil {
		return "", "", hongguoRegisterRejection{errors.New("红果设备注册返回格式异常")}
	}
	deviceID, installID := mapString(result, "device_id_str", "device_id"), mapString(result, "install_id_str", "install_id")
	if !hongguoNumericID.MatchString(deviceID) || !hongguoNumericID.MatchString(installID) || deviceID == "0" || installID == "0" {
		return "", "", hongguoRegisterRejection{errors.New("红果设备注册未返回设备号")}
	}
	return deviceID, installID, nil
}


// hongguoDevicePool 是设备池的持久化格式，独立成文件以便注册后立即落盘。
type hongguoDevicePool struct {
	Version    int             `json:"version"`
	DeviceID   string          `json:"deviceId"`
	InstallID  string          `json:"installId"`
	Registered bool            `json:"registered"`
	Devices    []hongguoDevice `json:"devices"`
}

func hongguoDevicePoolPath(directory string) string {
	if directory == "" {
		return ""
	}
	return filepath.Join(directory, hongguoDevicePoolFile)
}

func loadHongguoDevicePool(directory string) *hongguoDevicePool {
	path := hongguoDevicePoolPath(directory)
	if path == "" {
		return nil
	}
	content, err := os.ReadFile(path)
	if err != nil || len(content) > 64<<10 {
		return nil
	}
	var pool hongguoDevicePool
	if json.Unmarshal(content, &pool) != nil || pool.Version != 1 {
		return nil
	}
	return &pool
}

func saveHongguoDevicePool(directory string, pool hongguoDevicePool) error {
	path := hongguoDevicePoolPath(directory)
	if path == "" {
		return nil
	}
	content, err := json.Marshal(pool)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(directory, hongguoDevicePoolFile+".*")
	if err != nil {
		return err
	}
	name := temp.Name()
	if _, err := temp.Write(content); err != nil {
		temp.Close()
		os.Remove(name)
		return err
	}
	if err := temp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// persistHongguoDevicePool 把当前设备池写入数据目录；失败只记录日志，不影响本次请求。
func (downloader *Downloader) persistHongguoDevicePool() {
	directory := downloader.cfg.dataDirectory()
	if directory == "" {
		return
	}
	client := downloader.hongguoClient()
	client.mu.Lock()
	pool := hongguoDevicePool{
		Version: 1, DeviceID: client.state.DeviceID, InstallID: client.state.InstallID,
		Registered: client.state.Registered, Devices: append([]hongguoDevice(nil), client.state.Devices...),
	}
	client.mu.Unlock()
	if err := saveHongguoDevicePool(directory, pool); err != nil {
		downloader.recordDiagnostic(diagnosticEvent{Level: "warn", Event: "provider.hongguo.device_pool",
			Source: sourceHongguo, Message: "设备池保存失败：" + publicError(err).Error()})
	}
}

// restoreHongguoDevicePool 在首次使用 App 客户端时恢复设备池，避免每次启动都重新注册。
func (downloader *Downloader) restoreHongguoDevicePool(client *hongguoAppClient) {
	pool := loadHongguoDevicePool(downloader.cfg.dataDirectory())
	if pool == nil || !pool.Registered || !hongguoNumericID.MatchString(pool.DeviceID) || !hongguoNumericID.MatchString(pool.InstallID) {
		return
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	client.state.DeviceID, client.state.InstallID, client.state.Registered = pool.DeviceID, pool.InstallID, true
	client.state.Devices = append([]hongguoDevice(nil), pool.Devices...)
	normalizeHongguoDevicesLocked(client)
}

const hongguoMaxDevices = 3

func validHongguoDevice(device hongguoDevice) bool {
	return hongguoNumericID.MatchString(device.DeviceID) && hongguoNumericID.MatchString(device.InstallID) &&
		device.DeviceID != "0" && device.InstallID != "0"
}

func normalizeHongguoDevicesLocked(client *hongguoAppClient) {
	if client.state.Registered {
		current := hongguoDevice{DeviceID: client.state.DeviceID, InstallID: client.state.InstallID}
		if validHongguoDevice(current) {
			exists := false
			for _, device := range client.state.Devices {
				if device.DeviceID == current.DeviceID {
					exists = true
					break
				}
			}
			if !exists {
				client.state.Devices = append(client.state.Devices, current)
			}
		}
	}
	seen := map[string]bool{}
	devices := make([]hongguoDevice, 0, len(client.state.Devices))
	for _, device := range client.state.Devices {
		if !validHongguoDevice(device) || seen[device.DeviceID] {
			continue
		}
		seen[device.DeviceID] = true
		devices = append(devices, device)
	}
	if len(devices) > hongguoMaxDevices {
		devices = devices[len(devices)-hongguoMaxDevices:]
	}
	client.state.Devices = devices
}

func useHongguoDeviceLocked(client *hongguoAppClient, rejected string) bool {
	normalizeHongguoDevicesLocked(client)
	for _, device := range client.state.Devices {
		if rejected != "" && device.DeviceID == rejected {
			continue
		}
		client.state.DeviceID, client.state.InstallID, client.state.Registered = device.DeviceID, device.InstallID, true
		return true
	}
	return false
}

func addHongguoDeviceLocked(client *hongguoAppClient, deviceID, installID string) {
	client.state.DeviceID, client.state.InstallID, client.state.Registered = deviceID, installID, true
	client.state.Devices = append(client.state.Devices, hongguoDevice{DeviceID: deviceID, InstallID: installID})
	normalizeHongguoDevicesLocked(client)
}
