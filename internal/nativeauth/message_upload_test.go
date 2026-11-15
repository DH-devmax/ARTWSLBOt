package nativeauth

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type uploadRoundTrip func(*http.Request) (*http.Response, error)

func (f uploadRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func syntheticWave() []byte {
	data := make([]byte, 44+16000)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1)
	binary.LittleEndian.PutUint16(data[22:], 1)
	binary.LittleEndian.PutUint32(data[24:], 8000)
	binary.LittleEndian.PutUint32(data[28:], 16000)
	binary.LittleEndian.PutUint16(data[32:], 2)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], 16000)
	return data
}

func syntheticMP4() []byte {
	box := func(kind string, body []byte) []byte {
		out := binary.BigEndian.AppendUint32(nil, uint32(len(body)+8))
		out = append(out, kind...)
		return append(out, body...)
	}
	mvhd, tkhd, hdlr := make([]byte, 20), make([]byte, 84), make([]byte, 12)
	binary.BigEndian.PutUint32(mvhd[12:], 1000)
	binary.BigEndian.PutUint32(mvhd[16:], 2000)
	binary.BigEndian.PutUint32(tkhd[76:], 320<<16)
	binary.BigEndian.PutUint32(tkhd[80:], 180<<16)
	copy(hdlr[8:], "vide")
	trak := box("trak", append(box("tkhd", tkhd), box("mdia", box("hdlr", hdlr))...))
	out := box("ftyp", []byte("isom\x00\x00\x02\x00isommp42"))
	out = append(out, box("moov", append(box("mvhd", mvhd), trak...))...)
	return append(out, box("mdat", []byte("synthetic-not-a-playable-codec"))...)
}

func TestMessageUploadDerivesMetadataAndBounds(t *testing.T) {
	var pngData bytes.Buffer
	png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 64, 32)))
	for _, fixture := range []struct {
		input    MessageUpload
		duration uint64
	}{
		{MessageUpload{Kind: "image", Name: "synthetic.png", Data: pngData.Bytes()}, 0},
		{MessageUpload{Kind: "voice", Name: "synthetic.wav", Data: syntheticWave()}, 1},
		{MessageUpload{Kind: "video", Name: "synthetic.mp4", Data: syntheticMP4(), Thumbnail: pngData.Bytes()}, 2},
		{MessageUpload{Kind: "file", Name: "synthetic.txt", Data: []byte("synthetic")}, 0},
	} {
		out, err := InspectUpload(fixture.input)
		if err != nil || out.Asset != "" || out.Duration != fixture.duration || out.Size != uint64(len(fixture.input.Data)) {
			t.Fatal("metadata not derived", fixture.input.Kind, err, out)
		}
	}
	for _, input := range []MessageUpload{
		{Kind: "file", Name: "../bad", Data: []byte("x")},
		{Kind: "voice", Name: "fake.wav", Data: []byte("RIFFinvalid")},
		{Kind: "video", Name: "fake.mp4", Data: syntheticMP4()},
		{Kind: "image", Name: "fake.png", Data: []byte("<svg/>")},
		{Kind: "file", Name: "large", Data: make([]byte, 17*1024*1024)},
	} {
		if _, err := InspectUpload(input); err == nil {
			t.Fatal("invalid accepted", input.Kind)
		}
	}
	bad := syntheticWave()
	binary.LittleEndian.PutUint32(bad[28:], 0)
	if _, err := waveDuration(bad); err == nil {
		t.Fatal("zero rate accepted")
	}
	bad = syntheticMP4()
	binary.BigEndian.PutUint32(bad[:4], 0xffffffff)
	if _, _, _, err := mp4Metadata(bad); err == nil {
		t.Fatal("oversized box accepted")
	}
}

func TestMessageUploadFixedOriginFenceAndUnknownAreOneShot(t *testing.T) {
	for _, mode := range []string{"success", "guard", "disconnect", "redirect", "rate", "invalid", "rejected"} {
		t.Run(mode, func(t *testing.T) {
			c := &Client{config: Deployment{ProtocolVersion: "2.8.3", Origin: "https://ifeng-api01.hfjtqy.com"}, jwt: "synthetic-jwt"}
			calls, guards := 0, 0
			c.http = &http.Client{Transport: uploadRoundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.String() != "https://authupapi01.hfjtqy.com/v1/assets/userupload" || r.Header.Get("token") != c.jwt {
					t.Fatal("credential sent to wrong origin")
				}
				reader, err := r.MultipartReader()
				if err != nil {
					t.Fatal(err)
				}
				fields := map[string]string{}
				for {
					part, e := reader.NextPart()
					if e == io.EOF {
						break
					}
					if e != nil {
						t.Fatal(e)
					}
					data, _ := io.ReadAll(part)
					fields[part.FormName()] = string(data)
				}
				if fields["asset_type"] != "4" || fields["use_for"] != "1" || fields["file"] != "synthetic" || fields["thumbnail"] != "" {
					t.Fatal("wrong form")
				}
				status, body := 200, `{"code":0,"id":"9007199254740993"}`
				header := make(http.Header)
				switch mode {
				case "disconnect":
					return nil, errors.New("synthetic disconnect")
				case "redirect":
					status = 307
					header.Set("Location", "https://not-allowed.example/")
				case "rate":
					status = 429
					header.Set("Retry-After", "8")
				case "invalid":
					body = `{"code":0,"id":"https://bad.example"}`
				case "rejected":
					body = `{"code":1001,"msg":"must not leak"}`
				}
				return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			out, err := c.UploadMessage(context.Background(), MessageUpload{Kind: "file", Name: "test.txt", Data: []byte("synthetic")}, func(context.Context) error {
				guards++
				if mode == "guard" {
					return errors.New("revoked")
				}
				return nil
			})
			if guards != 1 || calls > 1 || mode == "guard" && calls != 0 {
				t.Fatal("retry or bypass", calls, guards)
			}
			if mode == "success" {
				if err != nil || out.Asset != "9007199254740993" {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("failure accepted")
			}
			if mode == "rate" && c.cooldown.Before(time.Now().Add(7*time.Second)) {
				t.Fatal("cooldown lost")
			}
		})
	}
}

func TestVoiceUploadSendsValidatedMIMEAndKeepsOnlyBusinessNumber(t *testing.T) {
	c := &Client{config: Deployment{ProtocolVersion: "2.8.3", Origin: "https://ifeng-api01.hfjtqy.com"}, jwt: "synthetic"}
	calls := 0
	c.http = &http.Client{Transport: uploadRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		reader, err := r.MultipartReader()
		if err != nil {
			t.Fatal(err)
		}
		seen := false
		for {
			part, e := reader.NextPart()
			if e == io.EOF {
				break
			}
			if e != nil {
				t.Fatal(e)
			}
			if part.FormName() == "file" {
				seen = part.Header.Get("Content-Type") == "audio/wav" && part.FileName() == "synthetic.wav"
			}
			io.Copy(io.Discard, part)
		}
		if !seen {
			t.Fatal("validated voice bytes lost their media type")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(
			`{"code":1001,"msg":"synthetic secret must not escape","token":"synthetic-token"}`))}, nil
	})}
	_, err := c.UploadMessage(context.Background(), MessageUpload{Kind: "voice", Name: "synthetic.wav", Data: syntheticWave()},
		func(context.Context) error { return nil })
	var rejection *Error
	if !errors.As(err, &rejection) || rejection.Number != 1001 || !rejection.Submitted ||
		rejection.Code != "native_business_rejected" || strings.Contains(err.Error(), "secret") || calls != 1 {
		t.Fatal("voice rejection leaked input or retried", err, calls)
	}
}
