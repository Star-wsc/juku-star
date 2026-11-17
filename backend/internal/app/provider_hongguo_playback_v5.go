package app

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"math/bits"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// long: 7.4.3.32 播放链路只认 V5（Helios/Medusa）签名，按 guoapp-modules/hongguo 1.0.2 移植。
var hongguoV5Magic = mustHexBytes("2d4b4fca49750d433fb5ae2c226dcc56")
var hongguoV5SignKey = mustHexBytes("b23c2a470a20d105dc3f8c4438f8272ed46d6c89ffdcdedd5108698d26e9489d")
var hongguoV5Sbox = mustHexBytes("53290fe9e51f316f90f74a7e034d367784ab49237cd0135cdc632c322fd3be0e2624a417ecd9151a7ff64f60730a87f9445ec6ada2f865d22b696c4188f24c0df37b75510b939f6bc2fe6883e68bc8c722181ba0bdb08c9871b50c9a16b46eea8eed20ba4b8a14e0fcf1468dbbef05a93e5a9d7dcf06898635c1fd99f5de287a80c594daaab19b6279fbd75070cb52eb2555c456e39e7839853874e2f0013ac0b64742d8ca10e4b79c955de8b843ce4ed464a6a72ed508091c92e7b3eedd1e1dd12afac321618fa3d65f9167c982f43d66a53c6a1997585b346dbf9672ff37cdbc073b2dae11453304df00815476b2a13fccdb30b957a8acafe127400248591238922563e36406d37a24100b7902a028044e218453c2a399199c9d13dc40c55ba67e502273850ebdc1a2e2188b8f2d9e3cb909b8c46a2a9f4a6dd93007eafa65b7b3a881aa908cc74f0ad76bbbb58617c37012ca05a469fd44e62eecf1e4272ce860c0ae5eadf99b20d8f0234174006f3f110d7657d01645d4eff8f71471cbfe1cb2f52648727bf3c9de55153e031b4d61757db63dbf7782eeafe9018a8891362fb14759ac32bab03b349aeb5a377c5156be4c8dfbe13a684bda39cf836697c894545fe0f2491acea9fc6252f4ff1e0fe72bd2df6e67a7f635e5bcc6d1a5dd951d89960887a18eb40c785cdb1f80ed42299333ab98cdd67f316cd54346585dccd759fb722789b08807cc0fda293f7e5e1819bb0353517020b31d3d00374952bea2c421e862040a9ec892a05b266b7a0d0cc316f483b81af39715657ffe32ad8de0c13077646035236ee5363e5f900e9a7cf850a81cd9960b6df73317226f99eaa6fff15cec34acb98246639b918ab2851466a7cdfc4ad1b52c2ae95a58d3734e47ddd5b7560193f644ef9c408b7dee39b4d6aabf4de761e3252d4c7ba3843187745d55e6a5db05d42b45caed43c0c5d0b64bfa9d94a4a9d8f2f0e1a108dcfd288fd2bc1fc7bd675412138169cb09249875ba3b68cfb11180afae104fc9789571c6dfce02413886796ae21e3c488cc21b9f423a066c2eabf98e2fdeeb5776e4f5d09a515ba58cbbab3e37b09fd4f6d9244df80512e84cd7956be301cf64f1429d58577cc241f3a2c655144f81aaf95a4e108da894b38970561ae9002619fbbac91fa922ed202bfc62cb08e0de2a7f3631435f8b47c8a3c563b4db4071ce74c46da6fa696a9e282d1e825d663b93bfd56c48dce1884bb10fccea063929f746443fd830a4232e53da21a1f2d1bdfebc3a02c10a1b8ad31d04fd616590d272ac737e98eb33af60528716bee78ec0ee8699031177277d9cff32542c67caf5f44591681335ae0e178f3409a7803db83892dde5b9b7f0c750e24aef077badd678a09b850b84ec1597e4963cb5e675df1c0d6fb618496ecd76c35c798325590c2fb27a5e")
var hongguoV5HashTable = [64]uint32{0xebb64faf, 0x07aadcc2, 0xcf3187bf, 0xe01138ff, 0x6d0bfcff, 0x5a30a3be, 0xb41ad638, 0x34180eb8, 0xf233eb6f, 0xb1a584cc, 0xccc30dc7, 0x47d1db51, 0xd55653de, 0x70a84fa1, 0x57473c12, 0xf76f0288, 0x2c077f0a, 0xda0dcad0, 0xfbb86f6c, 0xfdc4cf00, 0x688a020d, 0xe676c6a6, 0x8cd6338b, 0x1a3c8d0e, 0xcce8b06b, 0x6ad0ed0b, 0xa0522717, 0xdc71ac83, 0x2285db71, 0xd5b4dda6, 0x736f8650, 0x6560306c, 0x617ce2a6, 0xe423417e, 0x0a40e143, 0x544e4032, 0x88dffb2a, 0x716c1ae0, 0x4c467a88, 0x05b23bb3, 0xe1d0b866, 0xbaa3dcb8, 0xae3374d3, 0xc3381a50, 0x1702f75b, 0xfe6da368, 0xf0b4cf48, 0x4e0ffbb8, 0x72aad10d, 0x26c53a3d, 0xf2bce0f6, 0xb4557581, 0x4a257fdd, 0x8c3182a2, 0xab0b3b86, 0x3d5dfb14, 0x4f103634, 0xd37b52d7, 0x444eff16, 0xeb0a33d1, 0x6ca86f6e, 0x00284ba7, 0x08387cfa, 0x5fb37586}

const hongguoPlaybackBaseURL = "https://api5-normal-sinfonlinea.fqnovel.com"
const hongguoPlaybackPath = "/novel/player/multi_video_model/player/v1/"
const hongguoPlaybackUserAgent = "com.phoenix.read/73932 (Linux; U; Android 12; zh_CN_#Hans; PGT-AN10; Build/V417IR;tt-ok/3.12.13.20)"
const hongguoV5License = "646637794"

func mustHexBytes(value string) []byte {
	raw, err := hex.DecodeString(value)
	if err != nil {
		panic(err)
	}
	return raw
}

// hongguoPlaybackQuery 是 V5 播放链路的独立 query（版本 7.4.3.32）。
func hongguoPlaybackQuery(deviceID, installID string) url.Values {
	return url.Values{
		"aid": {"8662"}, "app_name": {"novelread"}, "version_code": {"74332"}, "version_name": {"7.4.3.32"},
		"channel": {"vivo_8662_64"}, "device_platform": {"android"}, "device_type": {"PGT-AN10"},
		"os_version": {"12"}, "update_version_code": {"74332"}, "iid": {installID},
		"cdid": {newHongguoUUID()}, "device_id": {deviceID},
	}
}

func newHongguoUUID() string {
	var raw [16]byte
	_, _ = rand.Read(raw[:])
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return hex.EncodeToString(raw[0:4]) + "-" + hex.EncodeToString(raw[4:6]) + "-" +
		hex.EncodeToString(raw[6:8]) + "-" + hex.EncodeToString(raw[8:10]) + "-" + hex.EncodeToString(raw[10:16])
}

func hongguoV5Rotl32(v uint32, n int) uint32 { return bits.RotateLeft32(v, n&31) }
func hongguoV5Rotl8(v uint8, n int) uint8    { return bits.RotateLeft8(v, n&7) }
func hongguoV5Rotl64(v uint64, n int) uint64 { return bits.RotateLeft64(v, ((n%64)+64)%64) }

func hongguoV5Le32(v uint32) []byte {
	return []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)}
}

func hongguoV5U32le(data []byte) uint32 {
	return uint32(data[0]) | uint32(data[1])<<8 | uint32(data[2])<<16 | uint32(data[3])<<24
}

func hongguoV5ReadU64(data []byte, off int) uint64 {
	return uint64(hongguoV5U32le(data[off:])) | uint64(hongguoV5U32le(data[off+4:]))<<32
}

func hongguoV5WriteU64(data []byte, off int, value uint64) {
	low, high := uint32(value), uint32(value>>32)
	for i := 0; i < 4; i++ {
		data[off+i] = byte(low >> (8 * i))
		data[off+4+i] = byte(high >> (8 * i))
	}
}

func hongguoV5Pad16(data []byte) []byte {
	padding := 16 - len(data)%16
	out := make([]byte, len(data)+padding)
	copy(out, data)
	for i := len(data); i < len(out); i++ {
		out[i] = byte(padding)
	}
	return out
}

func hongguoV5Concat(parts ...[]byte) []byte {
	total := 0
	for _, part := range parts {
		total += len(part)
	}
	out := make([]byte, 0, total)
	for _, part := range parts {
		out = append(out, part...)
	}
	return out
}

func hongguoV5AppendUvarint(out []byte, value uint64) []byte {
	for value >= 128 {
		out = append(out, byte(value%128)|128)
		value /= 128
	}
	return append(out, byte(value))
}

type hongguoV5Field struct {
	tag    int
	number uint64
	data   []byte
}

func hongguoV5Num(tag int, value uint64) hongguoV5Field {
	return hongguoV5Field{tag: tag, number: value}
}
func hongguoV5Bytes(tag int, value []byte) hongguoV5Field {
	return hongguoV5Field{tag: tag, data: value}
}
func hongguoV5Text(tag int, value string) hongguoV5Field {
	return hongguoV5Field{tag: tag, data: []byte(value)}
}

func hongguoV5Proto(fields []hongguoV5Field) []byte {
	var out []byte
	for _, field := range fields {
		if field.data != nil {
			if len(field.data) == 0 {
				continue
			}
			out = hongguoV5AppendUvarint(out, uint64(field.tag)*8+2)
			out = hongguoV5AppendUvarint(out, uint64(len(field.data)))
			out = append(out, field.data...)
		} else {
			out = hongguoV5AppendUvarint(out, uint64(field.tag)*8)
			out = hongguoV5AppendUvarint(out, field.number*2)
		}
	}
	return out
}

func hongguoV5IV(queryHash, bodyMD5 []byte, ts uint32) uint32 {
	value := uint32(0x20230928)
	for _, data := range [][]byte{queryHash, bodyMD5, hongguoV5Le32(ts)} {
		for i, b := range data {
			if i&1 == 0 {
				value = (value >> 4) ^ value ^ (value << 6) ^ uint32(b)
			} else {
				value = ^((value >> 7) ^ value ^ (uint32(b) | (value << 12)))
			}
		}
	}
	return value
}

func hongguoV5Hash(queryHash, bodyMD5 []byte, ts uint32) ([]byte, error) {
	if len(queryHash) != 32 || len(bodyMD5) != 16 {
		return nil, errors.New("V5 hash 输入长度错误")
	}
	if hongguoV5IV(queryHash, bodyMD5, ts)&15 != 12 {
		return nil, errors.New("不支持的 V5 hash 分支")
	}
	iv := uint32(0x9d4a97c2)
	count := iv + ts
	table := make([]uint32, 64)
	for i := 0; i < 64; i++ {
		table[i] = hongguoV5Rotl32(hongguoV5HashTable[i], -int((count+1)&255))
	}
	shift := (count + 2) & 7
	padding := make([]byte, 4)
	for i, value := range []uint32{0xfa, 0x45, 0x61, 0xd7} {
		padding[i] = byte(((value | (value << 8)) >> shift) & 255)
	}
	data := hongguoV5Concat(queryHash, bodyMD5, hongguoV5Le32(ts), padding,
		[]byte{0, 0, 0, 0, 0, 0, 1, 0xa0})
	words := make([]uint32, 0, 128)
	for i := 0; i < len(data); i += 4 {
		words = append(words, uint32(data[i])<<24|uint32(data[i+1])<<16|uint32(data[i+2])<<8|uint32(data[i+3]))
	}
	for i := 0; i < 112; i++ {
		x, y := words[i+1], words[i+14]
		words = append(words, words[i]+words[i+9]+
			(hongguoV5Rotl32(x, 14)^hongguoV5Rotl32(x, 25)^(x>>3))+
			(hongguoV5Rotl32(y, 13)^hongguoV5Rotl32(y, 15)^(y>>10)))
	}
	initial := make([]uint32, 8)
	for i, value := range []uint32{0x7aba4fc8, 0x67166507, 0x6403fa00, 0x340f512f,
		984304912, 3005047866, 2874125293, 2152413264} {
		initial[i] = hongguoV5Rotl32(value, -int(count))
	}
	state := append([]uint32(nil), initial...)
	for i := 0; i < 96; i++ {
		base := iv + uint32(i)
		choose := ((state[4] ^ state[0]) & state[1]) ^ state[4]
		sigma1 := hongguoV5Rotl32(state[1], 26) ^ hongguoV5Rotl32(state[1], 21) ^ hongguoV5Rotl32(state[1], 7)
		mixed := words[base&127] + choose + sigma1 + table[base&63] + state[6]
		sigma0 := hongguoV5Rotl32(state[3], 30) ^ hongguoV5Rotl32(state[3], 19) ^ hongguoV5Rotl32(state[3], 10)
		majority := (state[3] & state[5]) | ((state[3] | state[5]) & state[2])
		old := state[7]
		last := state[7]
		copy(state[1:], state[:7])
		state[0] = last
		state[7] = sigma0 + majority + mixed
		state[0] = old + mixed
	}
	folded := make([]byte, 32)
	for i := 0; i < 8; i++ {
		v := state[i] + initial[i]
		folded[i*4] = byte(v >> 24)
		folded[i*4+1] = byte(v >> 16)
		folded[i*4+2] = byte(v >> 8)
		folded[i*4+3] = byte(v)
	}
	out := make([]byte, 16)
	for i := 0; i < 16; i++ {
		out[i] = folded[i] ^ folded[i+16]
	}
	check := uint32(0x20220420)
	for i := 0; i < 12; i++ {
		even := i&1 == 0
		var temp uint32
		if even {
			temp = (check >> 3) ^ check
			check = (uint32(out[i]) ^ (check << 7)) ^ temp
		} else {
			temp = (check >> 5) ^ check
			check = ^(uint32(out[i]) | (check << 11)) ^ temp
		}
	}
	return hongguoV5Concat(out, hongguoV5Le32((check|4)^0x1000000|0x20)), nil
}

func hongguoV5Xt(v uint32) uint32 {
	if v&128 != 0 {
		return ((v << 1) ^ 0x1b) & 255
	}
	return (v << 1) & 255
}

func hongguoV5Carrier(keyBytes, ivBytes, source []byte, ts uint32) ([]byte, error) {
	if len(keyBytes) != 16 || len(ivBytes) != 16 || len(source) < 248 {
		return nil, errors.New("V5 carrier 输入长度错误")
	}
	variant := ts & 3
	box := make([]uint8, 256)
	for i := 0; i < 256; i++ {
		box[i] = hongguoV5Sbox[int(variant)*256+i]
	}
	cons := [4][4]int{{2, 0, 3, 1}, {1, 3, 2, 0}, {3, 2, 1, 0}, {2, 3, 1, 0}}
	con := cons[variant]
	orders := [4][16]int{
		{0, 9, 14, 15, 4, 13, 2, 7, 8, 1, 6, 3, 12, 5, 10, 11},
		{0, 9, 14, 11, 4, 13, 2, 3, 8, 1, 6, 15, 12, 5, 10, 7},
		{0, 9, 14, 3, 4, 13, 2, 15, 8, 1, 6, 11, 12, 5, 10, 7},
		{0, 9, 14, 15, 4, 13, 2, 3, 8, 1, 6, 11, 12, 5, 10, 7},
	}
	order := orders[variant]
	initials := [4]uint32{0xbbb5de72, 0xd4638d5b, 0x92a5f73b, 0x89f8a1c9}
	initial := initials[variant]
	initialBytes := hongguoV5Le32(initial)
	expanded := make([]uint32, 48)
	for i := 0; i < 16; i++ {
		expanded[i] = uint32(initialBytes[i&3]) ^ uint32(keyBytes[i])
	}
	rounds := 8
	for i := 4; i < 12; i++ {
		at := 4 * (i - 1)
		a, b, c, d := expanded[at], expanded[at+1], expanded[at+2], expanded[at+3]
		if i&3 == 0 {
			next := ((initial >> (rounds & 24)) ^ uint32(box[b])) & 255
			b, c, d, a = uint32(box[c]), uint32(box[d]), uint32(box[a]), next
		}
		rounds += 2
		expanded[at+4] = a ^ expanded[at-12]
		expanded[at+5] = b ^ expanded[at-11]
		expanded[at+6] = c ^ expanded[at-10]
		expanded[at+7] = d ^ expanded[at-9]
	}
	block := func(value []byte) []byte {
		rows := [4][4]uint32{}
		for i := 0; i < 4; i++ {
			for j := 0; j < 4; j++ {
				rows[i][j] = uint32(value[i*4+j])
			}
		}
		addCon := func(offset int) {
			for i := 0; i < 4; i++ {
				for j := 0; j < 4; j++ {
					rows[i][j] ^= expanded[(offset+i)*4+con[j]]
				}
			}
		}
		addCon(0)
		for round := 1; round <= 2; round++ {
			for i := 0; i < 4; i++ {
				for j := 0; j < 4; j++ {
					rows[i][j] = uint32(box[rows[i][j]])
				}
			}
			before := rows
			for i := 0; i < 4; i++ {
				rows[i] = before[con[i]]
			}
			var flat [16]uint32
			for i := 0; i < 4; i++ {
				copy(flat[i*4:], rows[i][:])
			}
			for index := 0; index < 16; index++ {
				rows[index/4][index%4] = flat[order[index]]
			}
			if round == 1 {
				reordered := [4][4]uint32{}
				for i := 0; i < 4; i++ {
					for j := 0; j < 4; j++ {
						reordered[i][j] = rows[i][con[j]]
					}
				}
				rows = reordered
				for index := 0; index < 4; index++ {
					mixed := rows[0][index] ^ rows[1][index] ^ rows[2][index] ^ rows[3][index]
					first := rows[0][index]
					rows[0][index] ^= mixed ^ hongguoV5Xt(rows[0][index]^rows[1][index])
					rows[1][index] ^= mixed ^ hongguoV5Xt(rows[1][index]^rows[2][index])
					rows[2][index] ^= mixed ^ hongguoV5Xt(rows[2][index]^rows[3][index])
					rows[3][index] ^= mixed ^ hongguoV5Xt(rows[3][index]^first)
				}
			}
			addCon(round * 4)
		}
		for i := 0; i < 4; i++ {
			for j := 0; j < 4; j++ {
				rows[i][j] ^= expanded[(4+i)*4+j]
			}
		}
		out := make([]byte, 16)
		for i := 0; i < 4; i++ {
			for j := 0; j < 4; j++ {
				out[i*4+j] = byte(rows[i][j])
			}
		}
		return out
	}
	wire := [8]int{6, 5, 3, 4, 2, 7, 1, 0}
	compact := [8]int{1, 7, 3, 6, 2, 0, 4, 5}
	plaintext := make([]uint32, 32)
	for i := 0; i < 31; i++ {
		for j := 0; j < 8; j++ {
			plaintext[i] |= ((uint32(source[i*8+j]) >> uint(wire[j])) & 1) << uint(compact[j])
		}
	}
	plaintext[31] = 1
	input := make([]uint32, 16)
	for i := 0; i < 16; i++ {
		input[i] = plaintext[i] ^ uint32(ivBytes[i])
	}
	first := block([]byte{byte(input[0]), byte(input[1]), byte(input[2]), byte(input[3]),
		byte(input[4]), byte(input[5]), byte(input[6]), byte(input[7]),
		byte(input[8]), byte(input[9]), byte(input[10]), byte(input[11]),
		byte(input[12]), byte(input[13]), byte(input[14]), byte(input[15])})
	for i := 0; i < 16; i++ {
		input[i] = plaintext[i+16] ^ uint32(first[i])
	}
	second := block([]byte{byte(input[0]), byte(input[1]), byte(input[2]), byte(input[3]),
		byte(input[4]), byte(input[5]), byte(input[6]), byte(input[7]),
		byte(input[8]), byte(input[9]), byte(input[10]), byte(input[11]),
		byte(input[12]), byte(input[13]), byte(input[14]), byte(input[15])})
	key2 := hongguoV5Concat(first, second)
	out := make([]byte, 0, 1+len(source))
	out = append(out, key2[31])
	out = append(out, source...)
	for i := 0; i < 31; i++ {
		for j := 0; j < 8; j++ {
			at := 1 + i*8 + j
			bit := uint8(1) << uint(wire[j])
			out[at] = (out[at] & ^bit) | byte(((uint32(key2[i])>>uint(compact[j]))&1)<<uint(wire[j]))
		}
	}
	return out, nil
}

func hongguoV5Xmxor(data, key []byte) []byte {
	encoded := make([]byte, len(data))
	for index := 0; index < len(data); index++ {
		at := (index * 4) & 28
		d0, d1 := uint32(key[at]), uint32(key[at+1])
		mixed := uint32(hongguoV5Rotl8(data[index], 4)) + d0
		mixed = (^mixed ^ d1) & 255
		mixed = uint32(hongguoV5Rotl8(uint8(mixed), 3))
		mixed = (mixed + d1) & 255
		mixed ^= d0
		encoded[len(data)-index-1] = byte(^mixed & 255)
	}
	lastIndex := len(encoded) - 1
	last := uint32(encoded[lastIndex]) ^ uint32(encoded[lastIndex-1])
	first := uint32(encoded[0])
	encoded[0] = byte((^last + first) & 255)
	encoded[1] = byte(((uint32(encoded[0]) ^ uint32(encoded[lastIndex]) ^ 254) + uint32(encoded[1])) & 255)
	encoded[2] = byte((uint32(encoded[2]) + ((last - first) ^ uint32(hongguoV5Rotl8(encoded[1], 3)) ^ 2)) & 255)
	for index := 0; index < len(encoded)-4; index++ {
		mixed := uint32(hongguoV5Rotl8(encoded[index+2], 3)) ^ uint32(encoded[index+1]) ^ uint32(index+3)
		encoded[index+3] = byte((^mixed + uint32(encoded[index+3])) & 255)
	}
	encoded[lastIndex] ^= encoded[lastIndex-1]
	sum := uint32(0)
	for index := 1; index < len(encoded); index++ {
		sum += uint32(encoded[index])
	}
	encoded[0] = byte(((uint32(encoded[0]) ^ uint32(encoded[1])) + sum) & 255)
	return encoded
}

// long: 测试可注入确定性随机源，生产默认 crypto/rand。
var hongguoV5Rand = func(out []byte) {
	_, _ = rand.Read(out)
}

func hongguoV5Random(n int) ([]byte, error) {
	out := make([]byte, n)
	hongguoV5Rand(out)
	return out, nil
}

func resetHongguoV5ProcessForTest() {
	hongguoV5Process.mu.Lock()
	hongguoV5Process.ready = false
	hongguoV5Process.mu.Unlock()
}

func hongguoV5Helios(ts uint32) (string, error) {
	prefix, err := hongguoV5Random(4)
	if err != nil {
		return "", err
	}
	sum := md5.Sum(hongguoV5Concat(prefix, []byte("8662")))
	ascii := []byte(hex.EncodeToString(sum[:]))
	left := hongguoV5ReadU64(ascii, 0)
	right := hongguoV5ReadU64(ascii, 8)
	queue := []uint64{hongguoV5ReadU64(ascii, 16), hongguoV5ReadU64(ascii, 24)}
	keys := make([]uint64, 34)
	for i := range keys {
		keys[i] = left
		mixed := hongguoV5Rotl64(right, -8) + left
		mixed ^= uint64(i)
		queue = append(queue, mixed)
		left = mixed ^ hongguoV5Rotl64(left, 3)
		right = queue[0]
		queue = queue[1:]
	}
	data := hongguoV5Pad16([]byte(strconv.FormatUint(uint64(ts), 10) + "-" + hongguoV5License + "-8662"))
	for offset := 0; offset < len(data); offset += 16 {
		a := hongguoV5ReadU64(data, offset)
		b := hongguoV5ReadU64(data, offset+8)
		for _, key := range keys {
			b = key ^ (a + hongguoV5Rotl64(b, -8))
			a = b ^ hongguoV5Rotl64(a, 3)
		}
		hongguoV5WriteU64(data, offset, a)
		hongguoV5WriteU64(data, offset+8, b)
	}
	return base64.StdEncoding.EncodeToString(hongguoV5Concat(prefix, data)), nil
}

type hongguoV5ProcessState struct {
	mu      sync.Mutex
	nonce   string
	started int64
	ready   bool
}

var hongguoV5Process hongguoV5ProcessState

func hongguoV5ProcessNonce(nowMs int64) (string, int64) {
	hongguoV5Process.mu.Lock()
	defer hongguoV5Process.mu.Unlock()
	if !hongguoV5Process.ready {
		raw, _ := hongguoV5Random(18)
		hongguoV5Process.nonce = base64.RawURLEncoding.EncodeToString(raw)
		hongguoV5Process.started = nowMs
		hongguoV5Process.ready = true
	}
	return hongguoV5Process.nonce, hongguoV5Process.started
}

func hongguoV5Medusa(queryHash, f13 []byte, ts uint32, who map[string]string, nowMs int64) (string, error) {
	nonceText, started := hongguoV5ProcessNonce(nowMs)
	flags := make([]byte, 8)
	deviceInfo := hongguoV5Proto([]hongguoV5Field{
		hongguoV5Num(1, 1), hongguoV5Num(2, 2), hongguoV5Text(3, "8662"), hongguoV5Text(4, who["device_id"]),
		hongguoV5Text(5, nonceText), hongguoV5Text(6, "!noperm!"), hongguoV5Text(12, "Asia/Shanghai,8"),
		hongguoV5Text(13, "zh_CN"), hongguoV5Text(22, who["os_version"]), hongguoV5Num(25, uint64(started)),
		hongguoV5Text(30, who["device_type"]), hongguoV5Text(31, who["device_brand"]),
		hongguoV5Text(32, who["device_type"]), hongguoV5Text(34, who["device_brand"]),
	})
	env := hongguoV5Proto([]hongguoV5Field{
		hongguoV5Text(6, "v04.10.03.04-bugfix"), hongguoV5Text(8, who["channel"]),
		hongguoV5Bytes(12, deviceInfo), hongguoV5Text(14, who["version_name"]),
	})
	randomField, err := hongguoV5Random(4)
	if err != nil {
		return "", err
	}
	message := hongguoV5Proto([]hongguoV5Field{
		hongguoV5Bytes(1, hongguoV5Magic), hongguoV5Num(2, 5),
		hongguoV5Num(3, uint64(hongguoV5U32le(randomField))), hongguoV5Text(4, "8662"),
		hongguoV5Text(5, who["device_id"]), hongguoV5Text(6, hongguoV5License),
		hongguoV5Text(7, who["version_name"]), hongguoV5Text(8, "v04.10.03-ml-android"),
		hongguoV5Num(9, 0x040a0300), hongguoV5Bytes(10, flags), hongguoV5Num(12, uint64(ts)),
		hongguoV5Bytes(13, f13), hongguoV5Bytes(14, queryHash[0:6]),
		hongguoV5Bytes(15, hongguoV5Proto([]hongguoV5Field{hongguoV5Num(1, 1), hongguoV5Num(2, 1), hongguoV5Num(3, 1), hongguoV5Num(5, 1)})),
		hongguoV5Num(17, uint64(ts)), hongguoV5Text(20, "none"), hongguoV5Num(21, 377),
		hongguoV5Bytes(23, env),
		hongguoV5Text(24, `{"cmr":16777216,"cmr2":16777216,"un_h":11,"vpn":0,"kd":0,"dyn":"","do":1,"tk":true}`),
	})
	nonceField, err := hongguoV5Random(4)
	if err != nil {
		return "", err
	}
	nonce := hongguoV5U32le(nonceField)
	hashSum := hongguoSM3(hongguoV5Concat(hongguoV5SignKey, hongguoV5Le32(nonce), hongguoV5SignKey))
	high := (nonce >> 16) & 255
	seedValue := ^(((high << 11) | (nonce >> 24)) ^ (high >> 5) ^ high)
	seed := hongguoV5Le32(seedValue)
	transformed := hongguoV5Concat(flags, hongguoV5Xmxor(message, hashSum[:]))
	for i, j := 0, len(transformed)-1; i < j; i, j = i+1, j-1 {
		transformed[i], transformed[j] = transformed[j], transformed[i]
	}
	for i := range transformed {
		transformed[i] ^= seed[(^uint32(i))&3]
	}
	check := uint32(0x18000001) | (uint32(queryHash[0])&63)<<14 | (uint32(f13[0])&63)<<8
	packedRandom, err := hongguoV5Random(4)
	if err != nil {
		return "", err
	}
	packed := hongguoV5Concat([]byte{0xca}, packedRandom, hongguoV5Le32(check), transformed,
		[]byte{byte(nonce >> 16), byte(nonce >> 24)})
	keySum, ivSum := md5.Sum(hongguoV5SignKey[:16]), md5.Sum(hongguoV5SignKey[16:32])
	encrypted, err := hongguoV5Carrier(keySum[:], ivSum[:], packed, ts)
	if err != nil {
		return "", err
	}
	prefix := hongguoV5Concat(hongguoV5Le32(5), hongguoV5Magic)
	stamp := hongguoV5Le32(ts)
	for i := range prefix {
		prefix[i] ^= stamp[i&3]
	}
	return base64.StdEncoding.EncodeToString(hongguoV5Concat(prefix,
		[]byte{byte(nonce), byte(nonce >> 8), 0, 1}, encrypted)), nil
}

// hongguoV5Sign 生成 V5 播放请求的完整 URL 与签名头。
func hongguoV5Sign(base string, values url.Values, body []byte, now time.Time) (string, map[string]string, error) {
	nowMs := now.UnixMilli()
	ts := uint32(now.Unix())
	bodyMD5 := make([]byte, 16)
	if body != nil {
		sum := md5.Sum(body)
		copy(bodyMD5, sum[:])
	}
	for offset := 0; offset < 1024; offset++ {
		params := url.Values{}
		for key, value := range values {
			params[key] = append([]string(nil), value...)
		}
		params.Set("_rticket", strconv.FormatInt(nowMs+int64(offset), 10))
		query := params.Encode()
		queryHash := hongguoSM3([]byte(query))
		if hongguoV5IV(queryHash[:], bodyMD5, ts)&15 != 12 {
			continue
		}
		who := map[string]string{
			"device_id": params.Get("device_id"), "version_name": params.Get("version_name"),
			"os_version": params.Get("os_version"), "device_type": params.Get("device_type"),
			"channel": params.Get("channel"), "device_brand": params.Get("device_brand"),
		}
		if who["device_brand"] == "" {
			who["device_brand"] = "HONOR"
		}
		f13, err := hongguoV5Hash(queryHash[:], bodyMD5, ts)
		if err != nil {
			return "", nil, err
		}
		helios, err := hongguoV5Helios(ts)
		if err != nil {
			return "", nil, err
		}
		medusa, err := hongguoV5Medusa(queryHash[:], f13, ts, who, nowMs)
		if err != nil {
			return "", nil, err
		}
		headers := map[string]string{
			"X-Khronos": strconv.FormatUint(uint64(ts), 10),
			"X-Helios":  helios,
			"X-Medusa":  medusa,
		}
		if body != nil {
			headers["X-SS-STUB"] = hexUpper(bodyMD5)
			headers["Content-Type"] = "application/json; charset=utf-8"
		}
		return base + "?" + query, headers, nil
	}
	return "", nil, errors.New("无法生成 V5 播放签名")
}

func hexUpper(value []byte) string {
	const digits = "0123456789ABCDEF"
	out := make([]byte, len(value)*2)
	for i, b := range value {
		out[i*2] = digits[b>>4]
		out[i*2+1] = digits[b&15]
	}
	return string(out)
}
