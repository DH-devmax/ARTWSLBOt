// SPDX-License-Identifier: BSD-3-Clause
//go:build linux

package cpu

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/shirou/gopsutil/v4/common"
)

func TestCPUInfoRejectsInt32Overflow(t *testing.T) {
	for _, field := range []string{"processor", "stepping", "cache size"} {
		for _, value := range []string{"2147483648", "-2147483649"} {
			t.Run(field+"/"+value, func(t *testing.T) {
				root := t.TempDir()
				data := "processor: 0\n" + field + ": " + value + "\n"
				if err := os.WriteFile(filepath.Join(root, "cpuinfo"), []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
				ctx := context.WithValue(context.Background(), common.EnvKey, common.EnvMap{
					common.HostProcEnvKey: root, common.HostSysEnvKey: root,
				})
				if _, err := InfoWithContext(ctx); !errors.Is(err, strconv.ErrRange) {
					t.Fatalf("expected a range error, got %v", err)
				}
			})
		}
	}
}

func TestCPUInfoRetainsInt32Boundary(t *testing.T) {
	root := t.TempDir()
	data := "processor: 2147483647\nstepping: 2147483647\ncache size: 2147483647 KB\n"
	if err := os.WriteFile(filepath.Join(root, "cpuinfo"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), common.EnvKey, common.EnvMap{
		common.HostProcEnvKey: root, common.HostSysEnvKey: root,
	})
	info, err := InfoWithContext(ctx)
	if err != nil || len(info) != 1 {
		t.Fatalf("boundary CPU info failed: %v, %v", info, err)
	}
	if info[0].CPU != 2147483647 || info[0].Stepping != 2147483647 || info[0].CacheSize != 2147483647 {
		t.Fatalf("boundary value changed: %+v", info[0])
	}
}
