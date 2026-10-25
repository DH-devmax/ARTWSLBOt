// SPDX-License-Identifier: AGPL-3.0-or-later
package gateway

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
)

type ReceivedFrame struct {
	Frame nativewire.Frame
	At    time.Time
}

type IMTransport struct {
	socket      *websocket.Conn
	mu          sync.Mutex
	serial      nativewire.SerialAllocator
	pending     map[[3]uint16]chan nativewire.Frame
	pushes      chan ReceivedFrame
	done        chan struct{}
	once        sync.Once
	err         error
	queuedBytes atomic.Int64
}

func discoverIM(ctx context.Context, client *http.Client, key, account string) ([]string, error) {
	u, _ := url.Parse("https://lbs.netease.im/lbs/webconf.jsp")
	q := url.Values{"id": {account}, "k": {key}, "sv": {"92114"}, "pv": {"1"},
		"networkType": {"0"}, "hostEnv": {"BROWSER"}}
	u.RawQuery = q.Encode()
	req, _ := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	response, err := client.Do(req)
	if err != nil {
		return nil, errors.New("im_discovery_failed")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || response.StatusCode != 200 || len(body) > 65536 {
		return nil, errors.New("im_discovery_failed")
	}
	var result struct {
		Common struct {
			Link    []string `json:"link"`
			Default []string `json:"link.default"`
		} `json:"common"`
	}
	if json.Unmarshal(body, &result) != nil {
		return nil, errors.New("im_discovery_failed")
	}
	endpoints := []string{}
	seen := map[string]bool{}
	for _, hosts := range [][]string{result.Common.Link, result.Common.Default} {
		for i, added := len(hosts)-1, 0; i >= 0 && added < 16; i, added = i-1, added+1 {
			host := hosts[i]
			if !strings.Contains(host, "://") {
				host = "https://" + host
			}
			u, err := url.Parse(host)
			if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil ||
				u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" ||
				strings.ContainsAny(host, "\\\r\n") {
				return nil, errors.New("im_discovery_endpoint")
			}
			u.Scheme, u.Path = "wss", "/websocket"
			if !seen[u.String()] {
				endpoints = append(endpoints, u.String())
				seen[u.String()] = true
			}
		}
	}
	if len(endpoints) == 0 {
		return nil, errors.New("im_discovery_failed")
	}
	return endpoints, nil
}

func connectIM(ctx context.Context, d nativeauth.Deployment, s nativeauth.Session, appKey string) (*IMTransport, error) {
	client := nativeauth.TransportHTTP()
	defer client.CloseIdleConnections()
	endpoints, err := discoverIM(ctx, client, appKey, s.NIMID)
	if err != nil {
		return nil, err
	}
	var identity [16]byte
	if _, err = rand.Read(identity[:]); err != nil {
		return nil, errors.New("im_identity_failed")
	}
	identity[6], identity[8] = identity[6]&15|64, identity[8]&63|128
	id := fmt.Sprintf("%x-%x-%x-%x-%x", identity[:4], identity[4:6], identity[6:8], identity[8:10], identity[10:])
	fields := []nativewire.Property{
		{Key: 3, Value: []byte("16")}, {Key: 4, Value: []byte("Linux")},
		{Key: 6, Value: []byte("92114")}, {Key: 8, Value: []byte("1")}, {Key: 9, Value: []byte("1")},
		{Key: 13, Value: []byte(d.DeviceID)}, {Key: 18, Value: []byte(appKey)},
		{Key: 19, Value: []byte(s.NIMID)}, {Key: 24, Value: []byte("ARTWSLBot")},
		{Key: 26, Value: []byte(id)}, {Key: 38, Value: []byte{}},
		{Key: 40, Value: []byte("9.21.14")}, {Key: 41, Value: []byte("0")},
		{Key: 42, Value: []byte("Native/9.21.14")}, {Key: 112, Value: []byte("0")},
		{Key: 1000, Value: []byte(s.NIMToken)},
	}
	if s.NIMAuthType != 0 {
		fields = append(fields, nativewire.Property{Key: 115, Value: []byte{byte('0' + s.NIMAuthType)}})
	}
	body, err := nativewire.Properties(fields)
	if err != nil {
		return nil, err
	}
	defer clear(body)
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second, NetDialContext: client.Transport.(*http.Transport).DialContext}
	for i, endpoint := range endpoints {
		if i >= 3 {
			break
		}
		t, err := openIM(ctx, &dialer, endpoint, body)
		if err == nil {
			return t, nil
		}
		if err.Error() == "im_auth_rejected" || err.Error() == "im_kicked" {
			return nil, err
		}
	}
	return nil, errors.New("im_connection_failed")
}

func openIM(ctx context.Context, dialer *websocket.Dialer, endpoint string, login []byte) (*IMTransport, error) {
	conn, response, err := dialer.DialContext(ctx, endpoint, nil)
	if response != nil && response.Body != nil {
		response.Body.Close()
	}
	if err != nil {
		return nil, errors.New("im_connection_failed")
	}
	t := &IMTransport{socket: conn, pending: make(map[[3]uint16]chan nativewire.Frame),
		pushes: make(chan ReceivedFrame, 32), done: make(chan struct{})}
	conn.SetReadLimit(nativewire.Limit)
	go t.read()
	go func() {
		select {
		case <-ctx.Done():
			t.close(ctx.Err())
		case <-t.done:
		}
	}()
	reply, err := t.request(ctx, 2, 3, login)
	if err == nil && reply.Code != 200 {
		err = errors.New("im_auth_rejected")
	}
	if err == nil {
		err = nativewire.ValidateIMLogin(reply.Body)
	}
	if err == nil {
		_, err = t.heartbeat(ctx)
	}
	if err != nil {
		t.close(err)
		return nil, err
	}
	return t, nil
}

func (t *IMTransport) close(err error) {
	t.once.Do(func() {
		t.mu.Lock()
		t.err = err
		t.mu.Unlock()
		close(t.done)
		t.socket.Close()
	})
}

func (t *IMTransport) read() {
	for {
		_ = t.socket.SetReadDeadline(time.Now().Add(40 * time.Second))
		kind, raw, err := t.socket.ReadMessage()
		at := time.Now()
		if err != nil || kind != websocket.BinaryMessage {
			t.close(errors.New("im_disconnected"))
			return
		}
		f, err := nativewire.ReadPacket(raw)
		if err != nil {
			t.close(nativewire.ErrPushBoundary)
			return
		}
		if f.Service == 2 && f.Command == 5 {
			t.close(errors.New("im_kicked"))
			return
		}
		t.mu.Lock()
		reply := t.pending[[3]uint16{uint16(f.Service), uint16(f.Command), f.Serial}]
		t.mu.Unlock()
		// Sync replies enter the same FIFO as pushes before the waiter wakes.
		// The consumer cannot advance a cursor ahead of an earlier message.
		if reply == nil || f.Service == 5 && f.Command == 1 {
			if t.queuedBytes.Add(int64(len(f.Body)+256)) > 16*nativewire.Limit {
				t.queuedBytes.Add(-int64(len(f.Body) + 256))
				t.close(ErrBackpressure)
				return
			}
			timer := time.NewTimer(5 * time.Second)
			select {
			case t.pushes <- ReceivedFrame{Frame: f, At: at}:
				timer.Stop()
			case <-timer.C:
				t.queuedBytes.Add(-int64(len(f.Body) + 256))
				t.close(ErrBackpressure)
				return
			case <-t.done:
				t.queuedBytes.Add(-int64(len(f.Body) + 256))
				timer.Stop()
				return
			}
		}
		if reply != nil {
			select {
			case reply <- f:
			default:
				t.close(nativewire.ErrPushBoundary)
				return
			}
		}
	}
}

func (t *IMTransport) write(ctx context.Context, service, command byte, body []byte, reply chan nativewire.Frame) ([3]uint16, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	key := [3]uint16{uint16(service), uint16(command), 0}
	select {
	case <-t.done:
		return key, t.err
	default:
	}
	if err := ctx.Err(); err != nil {
		return key, err
	}
	serial, err := t.serial.Next()
	if err != nil {
		return key, err
	}
	key[2] = serial
	if reply != nil {
		t.pending[key] = reply
	}
	packet, err := nativewire.Packet(service, command, serial, body)
	if err == nil {
		_ = t.socket.SetWriteDeadline(time.Now().Add(5 * time.Second))
		err = t.socket.WriteMessage(websocket.BinaryMessage, packet)
	}
	if err != nil {
		delete(t.pending, key)
		return key, errors.New("im_write_failed")
	}
	return key, nil
}

func (t *IMTransport) request(ctx context.Context, service, command byte, body []byte) (nativewire.Frame, error) {
	wait := make(chan nativewire.Frame, 1)
	key, err := t.write(ctx, service, command, body, wait)
	if err != nil {
		return nativewire.Frame{}, err
	}
	defer func() { t.mu.Lock(); delete(t.pending, key); t.mu.Unlock() }()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case f := <-wait:
		return f, nil
	case <-t.done:
		t.mu.Lock()
		defer t.mu.Unlock()
		return nativewire.Frame{}, t.err
	case <-ctx.Done():
		return nativewire.Frame{}, ctx.Err()
	case <-timer.C:
		return nativewire.Frame{}, errors.New("im_reply_timeout")
	}
}

func (t *IMTransport) heartbeat(ctx context.Context) (time.Duration, error) {
	start := time.Now()
	reply, err := t.request(ctx, 1, 2, nil)
	if err != nil {
		return 0, err
	}
	if reply.Code != 200 || len(reply.Body) != 0 {
		return 0, errors.New("im_heartbeat_invalid")
	}
	return time.Since(start), nil
}
