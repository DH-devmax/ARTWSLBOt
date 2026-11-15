// SPDX-License-Identifier: BSD-3-Clause
//go:build linux

package mem

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/shirou/gopsutil/v4/common"
)

func TestAvailableMemoryWatermarkDoesNotUnderflow(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "zoneinfo"), []byte("low 2\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), common.EnvKey, common.EnvMap{common.HostProcEnvKey: root})
	page := uint64(os.Getpagesize())
	for _, tc := range []struct {
		name         string
		free, active uint64
		want         uint64
	}{
		{"below reserve", page, 0, 0},
		{"equal reserve", 2 * page, 0, 0},
		{"above reserve", 3 * page, 0, page},
		{"cache offsets deficit", page, 4 * page, page},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := calculateAvailVmem(ctx, &VirtualMemoryStat{Free: tc.free},
				&ExVirtualMemory{ActiveFile: tc.active})
			if got != tc.want {
				t.Fatalf("available memory = %d, want %d", got, tc.want)
			}
		})
	}
}
