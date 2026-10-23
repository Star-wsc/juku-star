package app

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"math/bits"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// hongguoModelSignKey 标记播放模型接口，该接口使用新版 X-Gorgon 签名；
// 详情、目录等接口仍使用带 X-Argus/X-Ladon 的旧版签名。
type hongguoModelSignKey struct{}

func newHongguoDeviceID() string {
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return strconv.FormatUint(1_000_000_000_000_000_000+binary.BigEndian.Uint64(random[:])%8_000_000_000_000_000_000, 10)
}

// hongguoModelGorgon 生成新版播放接口的 X-Gorgon。参数必须与实际发送的
// RawQuery 和请求体完全一致，签名对字节级内容敏感。
func hongguoModelGorgon(query string, body []byte, timestamp, random uint32) string {
	queryHash := md5.Sum([]byte(query))
	var bodyHash [4]byte
	if body != nil {
		sum := md5.Sum(body)
		copy(bodyHash[:], sum[:4])
	}
	input := make([]byte, 0, 20)
	input = append(input, queryHash[:4]...)
	input = append(input, bodyHash[:]...)
	input = append(input, 0, 0, 0, 0)
	var sdk [4]byte
	binary.LittleEndian.PutUint32(sdk[:], 67503104)
	input = append(input, sdk[:]...)
	input = append(input, byte(timestamp>>24), byte(timestamp>>16), byte(timestamp>>8), byte(timestamp))
	key := [8]byte{0x4a, 0x40, 0x16, byte((random >> 8) & 255), 0x47, 0x6c, 1, byte(random & 255)}
	var box [256]byte
	for index := range box {
		box[index] = byte(index)
	}
	j := 0
	for index := 0; index < 256; index++ {
		j = (j + int(box[index]) + int(key[index%8])) & 255
		box[index] = box[j]
	}
	j = 0
	out := make([]byte, len(input))
	for index := range input {
		i := index + 1
		j += int(box[i])
		value := box[j&255]
		box[i] = value
		out[index] = input[index] ^ box[(int(value)+int(value))&255]
	}
	for index := range out {
		value := ((out[index] >> 4) | (out[index] << 4)) & 255
		next := out[(index+1)%len(out)]
		reversed := bits.Reverse8(next ^ value)
		out[index] = ^(reversed ^ 20)
	}
	signature := append([]byte{0x84, 0x04, byte(random & 255), byte((random >> 8) & 255), 0x40, 0x01}, out...)
	return hex.EncodeToString(signature)
}

func signHongguoModelRequest(request *http.Request, body []byte, now time.Time) {
	timestamp := uint32(now.Unix())
	var randomBytes [2]byte
	if _, err := rand.Read(randomBytes[:]); err != nil {
		binary.BigEndian.PutUint16(randomBytes[:], uint16(now.UnixNano()))
	}
	random := uint32(binary.BigEndian.Uint16(randomBytes[:]))
	if body != nil {
		sum := md5.Sum(body)
		request.Header.Set("X-SS-STUB", strings.ToUpper(hex.EncodeToString(sum[:])))
	}
	request.Header.Set("X-Khronos", strconv.FormatUint(uint64(timestamp), 10))
	request.Header.Set("X-Gorgon", hongguoModelGorgon(request.URL.RawQuery, body, timestamp, random))
	request.Header.Set("X-SS-Req-Ticket", strconv.FormatInt(now.UnixMilli(), 10))
	// 播放接口只校验 X-Gorgon；X-Argus/X-Ladon 使用与客户端一致的时间戳占位值。
	request.Header.Set("X-Ladon", base64.StdEncoding.EncodeToString([]byte{byte(timestamp >> 24), byte(timestamp >> 16), byte(timestamp >> 8), byte(timestamp)}))
	request.Header.Set("X-Argus", base64.StdEncoding.EncodeToString([]byte{byte(timestamp), byte(timestamp >> 8), byte(timestamp >> 16), byte(timestamp >> 24)}))
	request.Header.Set("X-Tt-Request-Tag", "t=0;n=0")
	request.Header.Set("Sdk-Version", "2")
	request.Header.Set("Passport-Sdk-Version", "50561")
	request.Header.Set("X-Xs-From-Web", "0")
	request.Header.Set("X-Vc-Bdturing-Sdk-Version", "3.7.2.cn")
}
