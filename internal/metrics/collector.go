package metrics

import (
	"context"
	"fmt"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
)

type Values struct {
	CPUUsage      float64 `json:"cpu_usage"`
	MemoryUsage   float64 `json:"memory_usage"`
	DiskUsage     float64 `json:"disk_usage"`
	LoadAverage   float64 `json:"load_average"`
	UptimeSeconds uint64  `json:"uptime_seconds"`
}

type Collector interface {
	Collect(context.Context, string) (Values, error)
}

type SystemCollector struct{}

func (SystemCollector) Collect(ctx context.Context, diskPath string) (Values, error) {
	cpuUsage, err := cpu.PercentWithContext(ctx, 0, false)
	if err != nil {
		return Values{}, fmt.Errorf("read CPU usage: %w", err)
	}
	if len(cpuUsage) == 0 {
		return Values{}, fmt.Errorf("read CPU usage: no data")
	}

	memory, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return Values{}, fmt.Errorf("read memory usage: %w", err)
	}

	diskUsage, err := disk.UsageWithContext(ctx, diskPath)
	if err != nil {
		return Values{}, fmt.Errorf("read disk usage for %q: %w", diskPath, err)
	}

	loadAverage, err := load.AvgWithContext(ctx)
	if err != nil {
		return Values{}, fmt.Errorf("read load average: %w", err)
	}

	uptime, err := host.UptimeWithContext(ctx)
	if err != nil {
		return Values{}, fmt.Errorf("read uptime: %w", err)
	}

	return Values{
		CPUUsage:      cpuUsage[0],
		MemoryUsage:   memory.UsedPercent,
		DiskUsage:     diskUsage.UsedPercent,
		LoadAverage:   loadAverage.Load1,
		UptimeSeconds: uptime,
	}, nil
}
