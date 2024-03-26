package main

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
)

var labelCname = []string{"container_name"}

type DockerCollector struct {
	cli *client.Client
}

func newDockerCollector() *DockerCollector {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Fatalf("can't create docker client: %v", err)
	}

	return &DockerCollector{
		cli: cli,
	}
}

func (c *DockerCollector) Describe(_ chan<- *prometheus.Desc) {

}

func (c *DockerCollector) Collect(ch chan<- prometheus.Metric) {
	containers, err := c.cli.ContainerList(context.Background(), container.ListOptions{
		All: true,
	})
	if err != nil {
		log.Error("can't list containers: ", err)
		return
	}

	var wg sync.WaitGroup

	for _, container := range containers {
		wg.Add(1)

		go c.processContainer(container, ch, &wg)
	}
	wg.Wait()
}

func (c *DockerCollector) processContainer(container types.Container, ch chan<- prometheus.Metric, wg *sync.WaitGroup) {
	defer wg.Done()
	cName := strings.TrimPrefix(strings.Join(container.Names, ";"), "/")
	var isRunning float64
	if container.State == "running" {
		isRunning = 1
	}

	// container state metric for all containers
	ch <- prometheus.MustNewConstMetric(prometheus.NewDesc(
		"dex_container_running",
		"1 if docker container is running, 0 otherwise",
		labelCname,
		nil,
	), prometheus.GaugeValue, isRunning, cName)

	// stats metrics only for running containers
	if isRunning == 1 {

		if stats, err := c.cli.ContainerStats(context.Background(), container.ID, false); err != nil {
			log.Fatal(err)
		} else {
			var containerStats types.StatsJSON
			err := json.NewDecoder(stats.Body).Decode(&containerStats)
			if err != nil {
				log.Error("can't read api stats: ", err)
			}
			if err := stats.Body.Close(); err != nil {
				log.Error("can't close body: ", err)
			}

			c.blockIoMetrics(ch, &containerStats, cName)

			c.memoryMetrics(ch, &containerStats, cName)

			c.networkMetrics(ch, &containerStats, cName)

			c.CPUMetrics(ch, &containerStats, cName)

			c.pidsMetrics(ch, &containerStats, cName)
		}
	}
}

func (c *DockerCollector) CPUMetrics(ch chan<- prometheus.Metric, containerStats *types.StatsJSON, cName string) {
	cpuDelta := containerStats.CPUStats.CPUUsage.TotalUsage - containerStats.PreCPUStats.CPUUsage.TotalUsage
	sysemDelta := containerStats.CPUStats.SystemUsage - containerStats.PreCPUStats.SystemUsage

	cpuUtilization := float64(cpuDelta) / float64(sysemDelta) * 100.0

	ch <- prometheus.MustNewConstMetric(prometheus.NewDesc(
		"dex_cpu_utilization_percent",
		"CPU utilization in percent",
		labelCname,
		nil,
	), prometheus.GaugeValue, cpuUtilization, cName)
}

func (c *DockerCollector) networkMetrics(ch chan<- prometheus.Metric, containerStats *types.StatsJSON, cName string) {
	ch <- prometheus.MustNewConstMetric(prometheus.NewDesc(
		"dex_network_rx_bytes",
		"Network received bytes total",
		labelCname,
		nil,
	), prometheus.CounterValue, float64(containerStats.Networks["eth0"].RxBytes), cName)
	ch <- prometheus.MustNewConstMetric(prometheus.NewDesc(
		"dex_network_tx_bytes",
		"Network sent bytes total",
		labelCname,
		nil,
	), prometheus.CounterValue, float64(containerStats.Networks["eth0"].TxBytes), cName)
}

func (c *DockerCollector) memoryMetrics(ch chan<- prometheus.Metric, containerStats *types.StatsJSON, cName string) {
	// From official documentation
	//Note: On Linux, the Docker CLI reports memory usage by subtracting page cache usage from the total memory usage.
	//The API does not perform such a calculation but rather provides the total memory usage and the amount from the page cache so that clients can use the data as needed.
	memoryUsage := containerStats.MemoryStats.Usage - containerStats.MemoryStats.Stats["cache"]
	memoryTotal := containerStats.MemoryStats.Limit

	memoryUtilization := float64(memoryUsage) / float64(memoryTotal) * 100.0
	ch <- prometheus.MustNewConstMetric(prometheus.NewDesc(
		"dex_memory_usage_bytes",
		"Total memory usage bytes",
		labelCname,
		nil,
	), prometheus.CounterValue, float64(memoryUsage), cName)
	ch <- prometheus.MustNewConstMetric(prometheus.NewDesc(
		"dex_memory_total_bytes",
		"Total memory bytes",
		labelCname,
		nil,
	), prometheus.CounterValue, float64(memoryTotal), cName)
	ch <- prometheus.MustNewConstMetric(prometheus.NewDesc(
		"dex_memory_utilization_percent",
		"Memory utilization percent",
		labelCname,
		nil,
	), prometheus.GaugeValue, memoryUtilization, cName)
}

func (c *DockerCollector) blockIoMetrics(ch chan<- prometheus.Metric, containerStats *types.StatsJSON, cName string) {
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
		"dex_block_io_read_bytes",
		"Block I/O read bytes",
		labelCname,
		nil,
	), prometheus.CounterValue, float64(readTotal), cName)

	ch <- prometheus.MustNewConstMetric(prometheus.NewDesc(
		"dex_block_io_write_bytes",
		"Block I/O write bytes",
		labelCname,
		nil,
	), prometheus.CounterValue, float64(writeTotal), cName)
}

func (c *DockerCollector) pidsMetrics(ch chan<- prometheus.Metric, containerStats *types.StatsJSON, cName string) {
	ch <- prometheus.MustNewConstMetric(prometheus.NewDesc(
		"dex_pids_current",
		"Current number of pids in the cgroup",
		labelCname,
		nil,
	), prometheus.CounterValue, float64(containerStats.PidsStats.Current), cName)
}
