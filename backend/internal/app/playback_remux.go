package app

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
)

type playbackRemuxKey struct{}

type playbackMP4Box struct {
	kind string
	body []byte
}

const playbackInitLimit = 256 * 1024

func readPlaybackMP4Init(reader io.Reader) ([]byte, string, error) {
	var initial []byte
	for len(initial) < playbackInitLimit {
		header := make([]byte, 8)
		if _, err := io.ReadFull(reader, header); err != nil {
			return nil, "", err
		}
		size := uint64(binary.BigEndian.Uint32(header))
		kind := string(header[4:8])
		if size == 1 {
			extended := make([]byte, 8)
			if _, err := io.ReadFull(reader, extended); err != nil {
				return nil, "", err
			}
			size = binary.BigEndian.Uint64(extended)
			header = append(header, extended...)
		}
		if size < uint64(len(header)) || size > uint64(playbackInitLimit-len(initial)) || kind != "ftyp" && kind != "free" && kind != "moov" {
			return nil, "", errors.New("播放初始化数据无效或过大")
		}
		body := make([]byte, int(size)-len(header))
		if _, err := io.ReadFull(reader, body); err != nil {
			return nil, "", err
		}
		initial = append(initial, header...)
		initial = append(initial, body...)
		if kind == "moov" {
			return initial, playbackRemuxMIME(body), nil
		}
	}
	return nil, "", errors.New("播放初始化数据过大")
}

func playbackMP4Boxes(data []byte) ([]playbackMP4Box, bool) {
	var boxes []playbackMP4Box
	for len(data) > 0 {
		if len(data) < 8 {
			return nil, false
		}
		size, header := uint64(binary.BigEndian.Uint32(data)), 8
		if size == 1 {
			if len(data) < 16 {
				return nil, false
			}
			size, header = binary.BigEndian.Uint64(data[8:16]), 16
		}
		if size < uint64(header) || size > uint64(len(data)) {
			return nil, false
		}
		boxes = append(boxes, playbackMP4Box{kind: string(data[4:8]), body: data[header:int(size)]})
		data = data[int(size):]
	}
	return boxes, true
}

func playbackMP4Child(data []byte, path ...string) []byte {
	for _, kind := range path {
		boxes, valid := playbackMP4Boxes(data)
		if !valid {
			return nil
		}
		data = nil
		for _, box := range boxes {
			if box.kind == kind {
				if data != nil {
					return nil
				}
				data = box.body
			}
		}
		if data == nil {
			return nil
		}
	}
	return data
}

func playbackRemuxMIME(moov []byte) string {
	boxes, valid := playbackMP4Boxes(moov)
	if !valid {
		return ""
	}
	var video string
	audio := false
	for _, track := range boxes {
		if track.kind != "trak" {
			continue
		}
		stsd := playbackMP4Child(track.body, "mdia", "minf", "stbl", "stsd")
		if len(stsd) < 8 || binary.BigEndian.Uint32(stsd[4:8]) != 1 {
			return ""
		}
		entries, valid := playbackMP4Boxes(stsd[8:])
		if !valid || len(entries) != 1 {
			return ""
		}
		entry := entries[0]
		switch entry.kind {
		case "avc1":
			if video != "" || len(entry.body) < 78 {
				return ""
			}
			width, height := int(binary.BigEndian.Uint16(entry.body[24:26])), int(binary.BigEndian.Uint16(entry.body[26:28]))
			if width == 0 || height == 0 || width > 1920 || height > 1920 || width*height > 1920*1080 {
				return ""
			}
			avcc := playbackMP4Child(entry.body[78:], "avcC")
			if len(avcc) < 7 || avcc[0] != 1 || avcc[1] != 66 && avcc[1] != 77 && avcc[1] != 100 || avcc[3] == 0 || avcc[3] > 42 {
				return ""
			}
			video = fmt.Sprintf("avc1.%02X%02X%02X", avcc[1], avcc[2], avcc[3])
		case "mp4a":
			if audio || len(entry.body) < 28 || binary.BigEndian.Uint16(entry.body[8:10]) != 0 {
				return ""
			}
			channels := binary.BigEndian.Uint16(entry.body[16:18])
			if channels < 1 || channels > 2 || !playbackAACLC(playbackMP4Child(entry.body[28:], "esds")) {
				return ""
			}
			audio = true
		default:
			return ""
		}
	}
	if video == "" {
		return ""
	}
	codecs := []string{video}
	if audio {
		codecs = append(codecs, "mp4a.40.2")
	}
	return `video/mp4; codecs="` + strings.Join(codecs, ", ") + `"`
}

func playbackMP4Descriptor(data []byte, tag byte) []byte {
	if len(data) < 2 || data[0] != tag {
		return nil
	}
	length := 0
	for i := 1; i < len(data) && i <= 4; i++ {
		length = length<<7 | int(data[i]&0x7f)
		if data[i]&0x80 == 0 {
			if length > len(data)-i-1 {
				return nil
			}
			return data[i+1 : i+1+length]
		}
	}
	return nil
}

func playbackAACLC(esds []byte) bool {
	if len(esds) < 4 {
		return false
	}
	es := playbackMP4Descriptor(esds[4:], 3)
	if len(es) < 3 {
		return false
	}
	flags, offset := es[2], 3
	if flags&0x80 != 0 {
		offset += 2
	}
	if flags&0x40 != 0 {
		if offset >= len(es) {
			return false
		}
		offset += 1 + int(es[offset])
	}
	if flags&0x20 != 0 {
		offset += 2
	}
	if offset >= len(es) {
		return false
	}
	decoder := playbackMP4Descriptor(es[offset:], 4)
	if len(decoder) < 13 || decoder[0] != 0x40 || decoder[1]>>2 != 5 {
		return false
	}
	config := playbackMP4Descriptor(decoder[13:], 5)
	return len(config) >= 2 && config[0]>>3 == 2
}
