//go:build linux

package metricsproc

import (
	"context"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/common"
	"github.com/shirou/gopsutil/v4/process"
	"github.com/tklauser/go-sysconf"
	"golang.org/x/sys/unix"

	"wangshangliao/internal/monitor"
)

type Reader struct{}

// gopsutil's Linux Times/CreateTime call virtualization discovery. Only its
// statm reader is used here; CPU/start use our own stat and CLOCK_BOOTTIME.
func (Reader) Read(ctx context.Context) (monitor.ProcessSample, error) {
	if err := ctx.Err(); err != nil {
		return monitor.ProcessSample{}, err
	}
	pid := os.Getpid()
	p := &process.Process{Pid: int32(pid)}
	ctx = context.WithValue(ctx, common.EnvKey, common.EnvMap{common.HostProcEnvKey: "/proc"})
	memory, err := p.MemoryInfoWithContext(ctx)
	if err != nil {
		return monitor.ProcessSample{}, err
	}
	file, err := os.Open("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return monitor.ProcessSample{}, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 16*1024+1))
	if err != nil || len(raw) > 16*1024 {
		return monitor.ProcessSample{}, errors.New("process stat unavailable")
	}
	ticks, err := sysconf.Sysconf(sysconf.SC_CLK_TCK)
	if err != nil {
		return monitor.ProcessSample{}, err
	}
	var boot unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_BOOTTIME, &boot); err != nil {
		return monitor.ProcessSample{}, err
	}
	cpu, started, err := parseOwnStat(raw, pid, ticks, time.Duration(boot.Nano()), time.Now())
	if err != nil {
		return monitor.ProcessSample{}, err
	}
	if err := ctx.Err(); err != nil {
		return monitor.ProcessSample{}, err
	}
	return monitor.ProcessSample{CPUSeconds: cpu, RSSBytes: memory.RSS, StartedAt: started}, nil
}

func parseOwnStat(raw []byte, pid int, ticks int64, uptime time.Duration, now time.Time) (float64, time.Time, error) {
	invalid := errors.New("invalid own process stat")
	text := string(raw)
	open, close := strings.IndexByte(text, '('), strings.LastIndexByte(text, ')')
	if open < 1 || close <= open || strings.TrimSpace(text[:open]) != strconv.Itoa(pid) || ticks <= 0 {
		return 0, time.Time{}, invalid
	}
	fields := strings.Fields(text[close+1:])
	if len(fields) < 20 {
		return 0, time.Time{}, invalid
	}
	user, errUser := strconv.ParseUint(fields[11], 10, 64)
	system, errSystem := strconv.ParseUint(fields[12], 10, 64)
	start, errStart := strconv.ParseUint(fields[19], 10, 64)
	if errUser != nil || errSystem != nil || errStart != nil || user > ^uint64(0)-system {
		return 0, time.Time{}, invalid
	}
	startSeconds := float64(start) / float64(ticks)
	if startSeconds > uptime.Seconds() || startSeconds < 0 {
		return 0, time.Time{}, invalid
	}
	age := uptime - time.Duration(startSeconds*float64(time.Second))
	return float64(user+system) / float64(ticks), now.Add(-age).UTC(), nil
}
