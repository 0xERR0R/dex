package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/prometheus/client_golang/prometheus"
)

var labelCname = []string{"container_name"}

type DockerCollector struct {
	cli    *client.Client
	labels []string
}

func newDockerCollector(labels []string) *DockerCollector {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		slog.Error("can't create docker client", "error", err)
		os.Exit(1)
	}

	return &DockerCollector{
		cli:    cli,
		labels: labels,
	}
}

func (c *DockerCollector) Describe(_ chan<- *prometheus.Desc) {

}

func (c *DockerCollector) Collect(ch chan<- prometheus.Metric) {
	containers, err := c.cli.ContainerList(context.Background(), container.ListOptions{
		All: true,
	})
	if err != nil {
		slog.Error("can't list containers", "error", err)
		return
	}

	var wg sync.WaitGroup

	for _, container := range containers {
		wg.Add(1)

		go c.processContainer(container, ch, &wg)
	}
	wg.Wait()
}

func (c *DockerCollector) getLabelValues(cont types.Container, cName string) []string {
	var labelValues []string
	labelValues = append(labelValues, cName)

	for _, label := range c.labels {
		switch label {
		case "image":
			labelValues = append(labelValues, cont.Image)
		case "image_id":
			labelValues = append(labelValues, cont.ID)
		case "command":
			labelValues = append(labelValues, cont.Command)
		case "created":
			labelValues = append(labelValues, strconv.FormatInt(cont.Created, 10))
		default:
			labelValues = append(labelValues, "")
			slog.Warn("label doesn't exist in container", "label", label, "container", cName)
		}
	}
	return labelValues
}

func (c *DockerCollector) processContainer(cont types.Container, ch chan<- prometheus.Metric, wg *sync.WaitGroup) {
	defer wg.Done()

	cName := strings.TrimPrefix(strings.Join(cont.Names, ";"), "/")

	labelNames := append(labelCname, c.labels...)

	labelValues := c.getLabelValues(cont, cName)

	var isRunning, isRestarting, isExited float64

	if cont.State == "running" {
		isRunning = 1
	}

	if cont.State == "restarting" {
		isRestarting = 1
	}

	if cont.State == "exited" {
		isExited = 1
	}

	// container state metric for all containers
	ch <- prometheus.MustNewConstMetric(prometheus.NewDesc(
		"dex_container_running",
		"1 if docker container is running, 0 otherwise",
		labelNames,
		nil,
	), prometheus.GaugeValue, isRunning, labelValues...)

	ch <- prometheus.MustNewConstMetric(prometheus.NewDesc(
		"dex_container_restarting",
		"1 if docker container is restarting, 0 otherwise",
		labelNames,
		nil,
	), prometheus.GaugeValue, isRestarting, labelValues...)

	ch <- prometheus.MustNewConstMetric(prometheus.NewDesc(
		"dex_container_exited",
		"1 if docker container exited, 0 otherwise",
		labelNames,
		nil,
	), prometheus.GaugeValue, isExited, labelValues...)

	if inspect, err := c.cli.ContainerInspect(context.Background(), cont.ID); err != nil {
		slog.Error("container inspect failed", "error", err)
		os.Exit(1)
	} else {
		ch <- prometheus.MustNewConstMetric(prometheus.NewDesc(
			"dex_container_restarts_total",
			"Number of times the container has restarted",
			labelNames,
			nil,
		), prometheus.CounterValue, float64(inspect.RestartCount), labelValues...)
	}

	// stats metrics only for running containers
	if isRunning == 1 {

		if stats, err := c.cli.ContainerStats(context.Background(), cont.ID, false); err != nil {
			slog.Error("container stats failed", "error", err)
			os.Exit(1)
		} else {
			var containerStats container.StatsResponse
			err := json.NewDecoder(stats.Body).Decode(&containerStats)
			if err != nil {
				slog.Error("can't read api stats", "error", err)
			}
			if err := stats.Body.Close(); err != nil {
				slog.Error("can't close body", "error", err)
			}

			c.blockIoMetrics(ch, &containerStats, labelNames, labelValues)

			c.memoryMetrics(ch, &containerStats, labelNames, labelValues)

			c.networkMetrics(ch, &containerStats, labelNames, labelValues)

			c.CPUMetrics(ch, &containerStats, labelNames, labelValues)

			c.pidsMetrics(ch, &containerStats, labelNames, labelValues)
		}
	}
}

func (c *DockerCollector) CPUMetrics(ch chan<- prometheus.Metric, containerStats *container.StatsResponse, labelNames []string, labelValues []string) {
	totalUsage := containerStats.CPUStats.CPUUsage.TotalUsage
	cpuDelta := totalUsage - containerStats.PreCPUStats.CPUUsage.TotalUsage
	sysemDelta := containerStats.CPUStats.SystemUsage - containerStats.PreCPUStats.SystemUsage
	onlineCPUs := containerStats.CPUStats.OnlineCPUs

	cpuUtilization := (float64(cpuDelta) / float64(sysemDelta)) * float64(onlineCPUs) * 100.0

	ch <- prometheus.MustNewConstMetric(prometheus.NewDesc(
		"dex_cpu_utilization_percent",
		"CPU utilization in percent",
		labelNames,
		nil,
	), prometheus.GaugeValue, cpuUtilization, labelValues...)

	ch <- prometheus.MustNewConstMetric(prometheus.NewDesc(
		"dex_cpu_utilization_seconds_total",
		"Cumulative CPU utilization in seconds",
		labelNames,
		nil,
	), prometheus.CounterValue, float64(totalUsage)/1e9, labelValues...)
}

func (c *DockerCollector) networkMetrics(ch chan<- prometheus.Metric, containerStats *container.StatsResponse, labelNames []string, labelValues []string) {
	ch <- prometheus.MustNewConstMetric(prometheus.NewDesc(
		"dex_network_rx_bytes_total",
		"Network received bytes total",
		labelNames,
		nil,
	), prometheus.CounterValue, float64(containerStats.Networks["eth0"].RxBytes), labelValues...)
	ch <- prometheus.MustNewConstMetric(prometheus.NewDesc(
		"dex_network_tx_bytes_total",
		"Network sent bytes total",
		labelNames,
		nil,
	), prometheus.CounterValue, float64(containerStats.Networks["eth0"].TxBytes), labelValues...)
}

func (c *DockerCollector) memoryMetrics(ch chan<- prometheus.Metric, containerStats *container.StatsResponse, labelNames []string, labelValues []string) {
	// From official documentation
	//Note: On Linux, the Docker CLI reports memory usage by subtracting page cache usage from the total memory usage.
	//The API does not perform such a calculation but rather provides the total memory usage and the amount from the page cache so that clients can use the data as needed.
	//On cgroup v1 hosts, the cache usage is defined as the value of total_inactive_file field in the memory.stat file.
	//On cgroup v2 hosts, the cache usage is defined as the value of inactive_file field.
	memoryUsage := containerStats.MemoryStats.Usage - getCacheMemory(containerStats.MemoryStats.Stats)
	memoryTotal := containerStats.MemoryStats.Limit

	memoryUtilization := float64(memoryUsage) / float64(memoryTotal) * 100.0
	ch <- prometheus.MustNewConstMetric(prometheus.NewDesc(
		"dex_memory_usage_bytes",
		"Total memory usage bytes",
		labelNames,
		nil,
	), prometheus.CounterValue, float64(memoryUsage), labelValues...)
	ch <- prometheus.MustNewConstMetric(prometheus.NewDesc(
		"dex_memory_total_bytes",
		"Total memory bytes",
		labelNames,
		nil,
	), prometheus.GaugeValue, float64(memoryTotal), labelValues...)
	ch <- prometheus.MustNewConstMetric(prometheus.NewDesc(
		"dex_memory_utilization_percent",
		"Memory utilization percent",
		labelNames,
		nil,
	), prometheus.GaugeValue, memoryUtilization, labelValues...)
}

func (c *DockerCollector) blockIoMetrics(ch chan<- prometheus.Metric, containerStats *container.StatsResponse, labelNames []string, labelValues []string) {
	var readTotal, writeTotal uint64
	for _, b := range containerStats.BlkioStats.IoServiceBytesRecursive {
		if strings.EqualFold(b.Op, "read") {
			readTotal += b.Value
		}
		if strings.EqualFold(b.Op, "write") {
			writeTotal += b.Value
		}
	}

	ch <- prometheus.MustNewConstMetric(prometheus.NewDesc(
		"dex_block_io_read_bytes_total",
		"Block I/O read bytes",
		labelNames,
		nil,
	), prometheus.CounterValue, float64(readTotal), labelValues...)

	ch <- prometheus.MustNewConstMetric(prometheus.NewDesc(
		"dex_block_io_write_bytes_total",
		"Block I/O write bytes",
		labelNames,
		nil,
	), prometheus.CounterValue, float64(writeTotal), labelValues...)
}

func (c *DockerCollector) pidsMetrics(ch chan<- prometheus.Metric, containerStats *container.StatsResponse, labelNames []string, labelValues []string) {
	ch <- prometheus.MustNewConstMetric(prometheus.NewDesc(
		"dex_pids_current",
		"Current number of pids in the cgroup",
		labelNames,
		nil,
	), prometheus.CounterValue, float64(containerStats.PidsStats.Current), labelValues...)
}

func getCacheMemory(stats map[string]uint64) uint64 {
	// On cgroup v2 hosts, the cache usage is defined as the value of inactive_file field.
	if val, ok := stats["inactive_file"]; ok {
		return val
	}
	// On cgroup v1 hosts, the cache usage is defined as the value of total_inactive_file field.
	if val, ok := stats["total_inactive_file"]; ok {
		return val
	}
	// Fallback for older versions
	return stats["cache"]
}
