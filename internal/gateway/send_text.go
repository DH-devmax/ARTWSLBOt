package gateway

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"wangshangliao/internal/nativewire"
)

// sendGroupText reuses the existing socket/reader/serial namespace. It does not
// dial, renew, retry, read a member directory or create a second connection.
// No production caller is wired until the control/identity acceptance passes.
func (t *IMTransport) sendGroupText(ctx context.Context, team, clientID, attachment string,
	fence nativewire.SubmissionFence) (nativewire.TextSendResult, error) {
	return t.sendGroupTextReply(ctx, team, clientID, attachment, nil, fence)
}

func (t *IMTransport) sendGroupTextReply(ctx context.Context, team, clientID, attachment string,
	reply *nativewire.ReplyRoute, fence nativewire.SubmissionFence) (nativewire.TextSendResult, error) {
	body, err := nativewire.GroupTextReplyRequest(team, clientID, attachment, reply)
	if err != nil {
		return nativewire.TextSendResult{}, err
	}
	return t.sendTextRequest(ctx, 8, 2, clientID, body, fence)
}

// Private text has its own NIM command and scene; it must not reuse a group
// target. No website endpoint is enabled by this transport primitive.
func (t *IMTransport) sendPrivateText(ctx context.Context, peer, clientID, attachment string,
	fence nativewire.SubmissionFence) (nativewire.TextSendResult, error) {
	body, err := nativewire.PrivateTextRequest(peer, clientID, attachment)
	if err != nil {
		return nativewire.TextSendResult{}, err
	}
	return t.sendTextRequest(ctx, 7, 1, clientID, body, fence)
}

func (t *IMTransport) sendTextRequest(ctx context.Context, service, command byte, clientID string, body []byte,
	fence nativewire.SubmissionFence) (nativewire.TextSendResult, error) {
	var result nativewire.TextSendResult
	defer clear(body)
	var err error
	if fence == nil {
		return result, nativewire.ErrProtocol
	}
	wait := make(chan nativewire.Frame, 1)
	var key [3]uint16
	// No transport lock is held while control acquires its bounded row locks.
	// Existing heartbeat/ACK and push reply matching can keep making progress.
	var called atomic.Bool
	err = fence(ctx, func(work context.Context) error {
		if !called.CompareAndSwap(false, true) {
			return errors.New("native_submission_repeated")
		}
		var e error
		key, result.Attempted, e = t.writeTracked(work, service, command, body, wait)
		return e
	})
	if !called.Load() && err == nil {
		err = errors.New("native_submission_not_authorized")
	}
	if err != nil {
		t.mu.Lock()
		delete(t.pending, key)
		t.mu.Unlock()
	}
	if err != nil {
		return result, err
	}
	defer func() { t.mu.Lock(); delete(t.pending, key); t.mu.Unlock() }()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case reply := <-wait:
		result.Code = reply.Code
		if reply.Code != 200 {
			return result, nil // explicit rejection; do not retain upstream body
		}
		result.Receipt, err = nativewire.ParseSendReceipt(reply.Body, clientID)
		if err != nil {
			return result, errors.New("im_send_receipt_invalid")
		}
		return result, nil
	case <-ctx.Done():
		return result, ctx.Err()
	case <-t.done:
		return result, errors.New("im_disconnected")
	case <-timer.C:
		return result, errors.New("im_send_receipt_timeout")
	}
}
