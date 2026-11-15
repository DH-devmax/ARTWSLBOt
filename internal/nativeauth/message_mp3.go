package nativeauth

import "bytes"

// Scan complete MPEG Layer III frames, including variable bitrates. This is a
// bounded container check, not a decoder. No media is transcoded or retained.
func mp3Duration(data []byte) (uint64, error) {
	invalid := rejection("native_upload_audio_invalid")
	if len(data) < 4 {
		return 0, invalid
	}
	offset := 0
	if bytes.HasPrefix(data, []byte("ID3")) {
		if len(data) < 10 || data[3] < 2 || data[3] > 4 || data[4] != 0 ||
			data[6]|data[7]|data[8]|data[9] >= 128 {
			return 0, invalid
		}
		size := int(data[6])<<21 | int(data[7])<<14 | int(data[8])<<7 | int(data[9])
		if size > 256*1024 {
			return 0, invalid
		}
		offset = 10 + size
		if data[3] == 4 && data[5]&0x10 != 0 {
			offset += 10
		}
		if offset > len(data) {
			return 0, invalid
		}
	}
	var samples, sampleRate uint64
	version := -1
	for frames := 0; offset < len(data); frames++ {
		if len(data)-offset == 128 && bytes.Equal(data[offset:offset+3], []byte("TAG")) {
			break
		}
		if frames >= 26000 || len(data)-offset < 4 {
			return 0, invalid
		}
		h := data[offset : offset+4]
		v, layer, bitrateIndex, rateIndex := int(h[1]>>3&3), h[1]>>1&3, h[2]>>4, h[2]>>2&3
		if h[0] != 0xff || h[1]&0xe0 != 0xe0 || v == 1 || layer != 1 ||
			bitrateIndex == 0 || bitrateIndex == 15 || rateIndex == 3 {
			return 0, invalid
		}
		rate := uint64([3]int{44100, 48000, 32000}[rateIndex])
		bitrate := uint64([14]int{32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320}[bitrateIndex-1]) * 1000
		frameSamples, coefficient := uint64(1152), uint64(144)
		if v != 3 {
			rate /= 2
			bitrate = uint64([14]int{8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160}[bitrateIndex-1]) * 1000
			frameSamples, coefficient = 576, 72
			if v == 0 {
				rate /= 2
			}
		}
		if sampleRate != 0 && (sampleRate != rate || version != v) {
			return 0, invalid
		}
		sampleRate, version = rate, v
		size := coefficient*bitrate/rate + uint64(h[2]>>1&1)
		if size < 4 || size > uint64(len(data)-offset) {
			return 0, invalid
		}
		offset += int(size)
		samples += frameSamples
		if samples > 300*sampleRate {
			return 0, invalid
		}
	}
	if samples == 0 || sampleRate == 0 {
		return 0, invalid
	}
	return (samples + sampleRate - 1) / sampleRate, nil
}
