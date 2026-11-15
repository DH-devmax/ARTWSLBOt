package nativeauth

import (
	"context"
	"sync"

	"wangshangliao/internal/nativewire"
)

// A repeated fence callback observes the first attempt, never another deletion.
func friendDeleteFence(fence nativewire.SubmissionFence) nativewire.SubmissionFence {
	return func(ctx context.Context, submit func(context.Context) error) error {
		var once sync.Once
		var attempted bool
		var result error
		err := fence(ctx, func(work context.Context) error {
			once.Do(func() {
				if result = work.Err(); result == nil {
					attempted = true
					result = submit(work)
				}
			})
			return result
		})
		if err != nil {
			return err
		}
		if !attempted && result == nil {
			return rejection("native_setting_fence_required")
		}
		return result
	}
}
