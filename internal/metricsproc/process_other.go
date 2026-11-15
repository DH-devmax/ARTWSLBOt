//go:build !linux

package metricsproc

import (
	"context"
	"errors"

	"wangshangliao/internal/monitor"
)

type Reader struct{}

func (Reader) Read(context.Context) (monitor.ProcessSample, error) {
	return monitor.ProcessSample{}, errors.New("process_metrics_linux_only")
}
