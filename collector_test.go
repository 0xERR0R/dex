package main

import (
	"strings"
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCPUMetrics(t *testing.T) {
	c := &DockerCollector{} // We don't need a real client for this test
	containerName := "test-container"

	stats := &container.StatsResponse{
		CPUStats: container.CPUStats{
			CPUUsage: container.CPUUsage{
				TotalUsage: 1000000000, // 1 second in nanoseconds
			},
			SystemUsage: 60000000000, // Example system usage
		},
		PreCPUStats: container.CPUStats{
			CPUUsage: container.CPUUsage{
				TotalUsage: 500000000, // 0.5 seconds in nanoseconds
			},
			SystemUsage: 50000000000, // Example previous system usage
		},
	}

	ch := make(chan prometheus.Metric, 2) // Expecting 2 metrics

	c.CPUMetrics(ch, stats, containerName)
	close(ch)

	var metrics []prometheus.Metric
	for metric := range ch {
		metrics = append(metrics, metric)
	}

	assert.Len(t, metrics, 2, "Expected 2 CPU metrics")

	expectedUtilizationPercent := 5.0
	expectedUtilizationSecondsTotal := 1.0

	foundUtilizationPercent := false
	foundUtilizationSecondsTotal := false

	for _, m := range metrics {
		desc := m.Desc().String()
		pbMetric := &dto.Metric{}
		err := m.Write(pbMetric)
		require.NoError(t, err, "Failed to write metric to protobuf")

		if strings.Contains(desc, "dex_cpu_utilization_percent") {
			foundUtilizationPercent = true
			require.NotNil(t, pbMetric.Gauge, "Gauge should not be nil for dex_cpu_utilization_percent")
			val := *pbMetric.Gauge.Value
			assert.InDelta(t, expectedUtilizationPercent, val, 0.001, "Unexpected dex_cpu_utilization_percent value")
		}
		if strings.Contains(desc, "dex_cpu_utilization_seconds_total") {
			foundUtilizationSecondsTotal = true
			require.NotNil(t, pbMetric.Counter, "Counter should not be nil for dex_cpu_utilization_seconds_total")
			val := *pbMetric.Counter.Value
			assert.InDelta(t, expectedUtilizationSecondsTotal, val, 0.001, "Unexpected dex_cpu_utilization_seconds_total value")
		}
	}

	assert.True(t, foundUtilizationPercent, "Metric dex_cpu_utilization_percent not found")
	assert.True(t, foundUtilizationSecondsTotal, "Metric dex_cpu_utilization_seconds_total not found")
}

func TestNetworkMetrics(t *testing.T) {
	c := &DockerCollector{}
	containerName := "test-network-container"

	stats := &container.StatsResponse{
		Networks: map[string]container.NetworkStats{
			"eth0": {
				RxBytes: 1024,
				TxBytes: 2048,
			},
		},
	}

	ch := make(chan prometheus.Metric, 2)
	c.networkMetrics(ch, stats, containerName)
	close(ch)

	var metrics []prometheus.Metric
	for metric := range ch {
		metrics = append(metrics, metric)
	}

	assert.Len(t, metrics, 2, "Expected 2 network metrics")

	expectedRxBytes := 1024.0
	expectedTxBytes := 2048.0

	foundRxBytes := false
	foundTxBytes := false

	for _, m := range metrics {
		desc := m.Desc().String()
		pbMetric := &dto.Metric{}
		err := m.Write(pbMetric)
		require.NoError(t, err, "Failed to write metric to protobuf")

		if strings.Contains(desc, "dex_network_rx_bytes_total") {
			foundRxBytes = true
			require.NotNil(t, pbMetric.Counter, "Counter should not be nil for dex_network_rx_bytes_total")
			val := *pbMetric.Counter.Value
			assert.Equal(t, expectedRxBytes, val, "Unexpected dex_network_rx_bytes_total value")
		}
		if strings.Contains(desc, "dex_network_tx_bytes_total") {
			foundTxBytes = true
			require.NotNil(t, pbMetric.Counter, "Counter should not be nil for dex_network_tx_bytes_total")
			val := *pbMetric.Counter.Value
			assert.Equal(t, expectedTxBytes, val, "Unexpected dex_network_tx_bytes_total value")
		}
	}

	assert.True(t, foundRxBytes, "Metric dex_network_rx_bytes_total not found")
	assert.True(t, foundTxBytes, "Metric dex_network_tx_bytes_total not found")
}

func TestMemoryMetrics(t *testing.T) {
	c := &DockerCollector{}
	containerName := "test-memory-container"

	stats := &container.StatsResponse{
		MemoryStats: container.MemoryStats{
			Usage: 800 * 1024 * 1024,  // 800 MiB
			Limit: 1024 * 1024 * 1024, // 1 GiB
			Stats: map[string]uint64{
				"cache": 200 * 1024 * 1024, // 200 MiB
			},
		},
	}

	ch := make(chan prometheus.Metric, 3)
	c.memoryMetrics(ch, stats, containerName)
	close(ch)

	var metrics []prometheus.Metric
	for metric := range ch {
		metrics = append(metrics, metric)
	}

	assert.Len(t, metrics, 3, "Expected 3 memory metrics")

	expectedMemoryUsageBytes := float64(600 * 1024 * 1024)
	expectedMemoryTotalBytes := float64(1024 * 1024 * 1024)
	expectedMemoryUtilizationPercent := (expectedMemoryUsageBytes / expectedMemoryTotalBytes) * 100.0

	foundUsageBytes := false
	foundTotalBytes := false
	foundUtilizationPercent := false

	for _, m := range metrics {
		desc := m.Desc().String()
		pbMetric := &dto.Metric{}
		err := m.Write(pbMetric)
		require.NoError(t, err, "Failed to write metric to protobuf")

		if strings.Contains(desc, "dex_memory_usage_bytes") {
			foundUsageBytes = true
			require.NotNil(t, pbMetric.Counter, "Counter should not be nil for dex_memory_usage_bytes")
			val := *pbMetric.Counter.Value
			assert.Equal(t, expectedMemoryUsageBytes, val, "Unexpected dex_memory_usage_bytes value")
		}
		if strings.Contains(desc, "dex_memory_total_bytes") {
			foundTotalBytes = true
			require.NotNil(t, pbMetric.Gauge, "Gauge should not be nil for dex_memory_total_bytes")
			val := *pbMetric.Gauge.Value
			assert.Equal(t, expectedMemoryTotalBytes, val, "Unexpected dex_memory_total_bytes value")
		}
		if strings.Contains(desc, "dex_memory_utilization_percent") {
			foundUtilizationPercent = true
			require.NotNil(t, pbMetric.Gauge, "Gauge should not be nil for dex_memory_utilization_percent")
			val := *pbMetric.Gauge.Value
			assert.InDelta(t, expectedMemoryUtilizationPercent, val, 0.001, "Unexpected dex_memory_utilization_percent value")
		}
	}

	assert.True(t, foundUsageBytes, "Metric dex_memory_usage_bytes not found")
	assert.True(t, foundTotalBytes, "Metric dex_memory_total_bytes not found")
	assert.True(t, foundUtilizationPercent, "Metric dex_memory_utilization_percent not found")
}

func TestBlockIoMetrics(t *testing.T) {
	c := &DockerCollector{}
	containerName := "test-blockio-container"

	stats := &container.StatsResponse{
		BlkioStats: container.BlkioStats{
			IoServiceBytesRecursive: []container.BlkioStatEntry{
				{Op: "Read", Value: 1000},
				{Op: "Write", Value: 2000},
				{Op: "Read", Value: 500},
				{Op: "Write", Value: 1000},
				{Op: "Total", Value: 4500}, // Should be ignored by current logic
			},
		},
	}

	ch := make(chan prometheus.Metric, 2)
	c.blockIoMetrics(ch, stats, containerName)
	close(ch)

	var metrics []prometheus.Metric
	for metric := range ch {
		metrics = append(metrics, metric)
	}

	assert.Len(t, metrics, 2, "Expected 2 block I/O metrics")

	expectedReadBytes := 1500.0
	expectedWriteBytes := 3000.0

	foundReadBytes := false
	foundWriteBytes := false

	for _, m := range metrics {
		desc := m.Desc().String()
		pbMetric := &dto.Metric{}
		err := m.Write(pbMetric)
		require.NoError(t, err, "Failed to write metric to protobuf")

		if strings.Contains(desc, "dex_block_io_read_bytes_total") {
			foundReadBytes = true
			require.NotNil(t, pbMetric.Counter, "Counter should not be nil for dex_block_io_read_bytes_total")
			val := *pbMetric.Counter.Value
			assert.Equal(t, expectedReadBytes, val, "Unexpected dex_block_io_read_bytes_total value")
		}
		if strings.Contains(desc, "dex_block_io_write_bytes_total") {
			foundWriteBytes = true
			require.NotNil(t, pbMetric.Counter, "Counter should not be nil for dex_block_io_write_bytes_total")
			val := *pbMetric.Counter.Value
			assert.Equal(t, expectedWriteBytes, val, "Unexpected dex_block_io_write_bytes_total value")
		}
	}

	assert.True(t, foundReadBytes, "Metric dex_block_io_read_bytes_total not found")
	assert.True(t, foundWriteBytes, "Metric dex_block_io_write_bytes_total not found")
}

func TestPidsMetrics(t *testing.T) {
	c := &DockerCollector{}
	containerName := "test-pids-container"

	stats := &container.StatsResponse{
		PidsStats: container.PidsStats{
			Current: 42,
		},
	}

	ch := make(chan prometheus.Metric, 1) // Expecting 1 metric
	c.pidsMetrics(ch, stats, containerName)
	close(ch)

	var metrics []prometheus.Metric
	for metric := range ch {
		metrics = append(metrics, metric)
	}

	assert.Len(t, metrics, 1, "Expected 1 PID metric")

	expectedPidsCurrent := 42.0
	foundPidsCurrent := false

	for _, m := range metrics {
		desc := m.Desc().String()
		pbMetric := &dto.Metric{}
		err := m.Write(pbMetric)
		require.NoError(t, err, "Failed to write metric to protobuf")

		if strings.Contains(desc, "dex_pids_current") {
			foundPidsCurrent = true
			require.NotNil(t, pbMetric.Counter, "Counter should not be nil for dex_pids_current")
			val := *pbMetric.Counter.Value
			assert.Equal(t, expectedPidsCurrent, val, "Unexpected dex_pids_current value")
		}
	}

	assert.True(t, foundPidsCurrent, "Metric dex_pids_current not found")
}
