package main

import (
	"context"
	"encoding/json"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	"strings"
	"sync"
)

var labelCname = []string{"container_name"}

type DockerCollector struct {
	cli *client.Client
}

func newDockerCollector() *DockerCollector {
	cli, err := client.NewEnvClient()
	if err != nil {
		log.Fatalf("can't create docker client: ", err)
	}
	return &DockerCollector{
		cli: cli,
	}
}

func (c *DockerCollector) Describe(_ chan<- *prometheus.Desc) {

}

func (c *DockerCollector) Collect(ch chan<- prometheus.Metric) {
	containers, err := c.cli.ContainerList(context.Background(), types.ContainerListOptions{
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

			var containerStats containerStats
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
		}
	}
}

func (c *DockerCollector) CPUMetrics(ch chan<- prometheus.Metric, containerStats *containerStats, cName string) {
	cpuDelta := containerStats.CPU.CPUUsage.TotalUsage - containerStats.PreCPU.CPUUsage.TotalUsage
	sysemDelta := containerStats.CPU.SystemCpuUsage - containerStats.PreCPU.SystemCpuUsage

	cpuUtilization := float64(cpuDelta) / float64(sysemDelta) * 100.0

	ch <- prometheus.MustNewConstMetric(prometheus.NewDesc(
		"dex_cpu_utilization_percent",
		"CPU utilization in percent",
		labelCname,
		nil,
	), prometheus.GaugeValue, cpuUtilization, cName)
}

func (c *DockerCollector) networkMetrics(ch chan<- prometheus.Metric, containerStats *containerStats, cName string) {
	ch <- prometheus.MustNewConstMetric(prometheus.NewDesc(
		"dex_network_rx_bytes",
		"Network received bytes total",
		labelCname,
		nil,
	), prometheus.CounterValue, float64(containerStats.Networks.Eth0.RxBytes), cName)
	ch <- prometheus.MustNewConstMetric(prometheus.NewDesc(
		"dex_network_tx_bytes",
		"Network sent bytes total",
		labelCname,
		nil,
	), prometheus.CounterValue, float64(containerStats.Networks.Eth0.TxBytes), cName)
}

func (c *DockerCollector) memoryMetrics(ch chan<- prometheus.Metric, containerStats *containerStats, cName string) {
	// From official documentation
	//Note: On Linux, the Docker CLI reports memory usage by subtracting page cache usage from the total memory usage.
	//The API does not perform such a calculation but rather provides the total memory usage and the amount from the page cache so that clients can use the data as needed.
	memoryUsage := containerStats.Memory.Usage - containerStats.Memory.MemoryStats.Cache

	memoryUtilization := float64(memoryUsage) / float64(containerStats.Memory.Limit) * 100.0
	ch <- prometheus.MustNewConstMetric(prometheus.NewDesc(
		"dex_memory_usage_bytes",
		"Total memory usage bytes",
		labelCname,
		nil,
	), prometheus.CounterValue, float64(memoryUsage), cName)
	ch <- prometheus.MustNewConstMetric(prometheus.NewDesc(
		"dex_memory_utilization_percent",
		"Memory utilization percent",
		labelCname,
		nil,
	), prometheus.GaugeValue, memoryUtilization, cName)
}

func (c *DockerCollector) blockIoMetrics(ch chan<- prometheus.Metric, containerStats *containerStats, cName string) {
	for _, b := range containerStats.BlockIO.IOBytes {
		if b.Op == "Read" {
			ch <- prometheus.MustNewConstMetric(prometheus.NewDesc(
				"dex_block_io_read_bytes",
				"Block I/O read bytes",
				labelCname,
				nil,
			), prometheus.CounterValue, float64(b.Value), cName)
		}
		if b.Op == "Write" {
			ch <- prometheus.MustNewConstMetric(prometheus.NewDesc(
				"dex_block_io_write_bytes",
				"Block I/O write bytes",
				labelCname,
				nil,
			), prometheus.CounterValue, float64(b.Value), cName)
		}
	}
}
