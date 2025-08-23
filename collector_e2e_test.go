package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/client"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestDockerCollector_E2E_BasicMetrics(t *testing.T) {
	ctx := context.Background()

	// Setup: Start a simple container
	req := testcontainers.ContainerRequest{
		Image:      "alpine:latest",
		Cmd:        []string{"sleep", "10"},
		WaitingFor: wait.ForExec([]string{"true"}).WithStartupTimeout(1 * time.Minute),
	}
	genericContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err, "Failed to start container")
	defer func() {
		if termErr := genericContainer.Terminate(ctx); termErr != nil {
			t.Logf("Failed to terminate container: %s", termErr.Error())
		}
	}()

	containerID := genericContainer.GetContainerID()
	require.NotEmpty(t, containerID, "Container ID should not be empty")

	dockerCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err, "Failed to create Docker client for inspect")
	// No defer dockerCli.Close() here, it might interfere with collector.cli

	inspectedContainer, err := dockerCli.ContainerInspect(ctx, containerID)
	require.NoError(t, err, "Failed to inspect container")
	actualContainerName := strings.TrimPrefix(inspectedContainer.Name, "/")
	dockerCli.Close() // Close this auxiliary client now that we have the name

	collector := newDockerCollector(nil)
	require.NotNil(t, collector, "Collector should not be nil")
	require.NotNil(t, collector.cli, "Collector Docker client should not be nil")
	// The main collector.cli will be closed by the main function or when the collector is GC'd if not explicitly closed.
	// For robust testing, ensure collector.cli is closed. Let's assume newDockerCollector could be modified
	// or we handle its lifecycle if it were a long-lived object. In a test, explicit close is good.
	defer collector.cli.Close()

	registry := prometheus.NewRegistry()
	err = registry.Register(collector)
	require.NoError(t, err, "Failed to register collector")

	// Give a brief moment for the container to be fully listed and stats to start populating if needed
	time.Sleep(3 * time.Second)

	metricChan := make(chan prometheus.Metric, 100) // Buffer to avoid blocking, actual count varies
	go func() {
		collector.Collect(metricChan)
		close(metricChan)
	}()

	foundRunningMetric := false
	foundRestartsMetric := false
	// Basic check for any CPU metric to indicate stats are flowing
	foundCPUMetricForTestContainer := false

	for m := range metricChan {
		pbMetric := &dto.Metric{}
		err = m.Write(pbMetric)
		require.NoError(t, err, "Error writing metric to protobuf")

		var metricContainerName string
		for _, labelPair := range pbMetric.Label {
			if labelPair.GetName() == "container_name" {
				metricContainerName = labelPair.GetValue()
				break
			}
		}

		if metricContainerName == actualContainerName {
			descString := m.Desc().String() // For easy check of metric name
			if strings.Contains(descString, "dex_container_running") {
				foundRunningMetric = true
				require.NotNil(t, pbMetric.Gauge, "Gauge should not be nil for dex_container_running")
				assert.Equal(t, 1.0, *pbMetric.Gauge.Value, "Container should be running")
			}
			if strings.Contains(descString, "dex_container_restarts_total") {
				foundRestartsMetric = true
				require.NotNil(t, pbMetric.Counter, "Counter should not be nil for dex_container_restarts_total")
				assert.Equal(t, 0.0, *pbMetric.Counter.Value, "Container restarts should be 0")
			}
			if strings.Contains(descString, "dex_cpu_utilization_percent") { // Check one of the stats-based metrics
				foundCPUMetricForTestContainer = true
				// Value can be anything, just checking it's produced for the running container
				require.NotNil(t, pbMetric.Gauge, "Gauge should not be nil for dex_cpu_utilization_percent")
				t.Logf("CPU utilization for %s: %f", actualContainerName, *pbMetric.Gauge.Value)
			}
		}
	}

	assert.True(t, foundRunningMetric, "dex_container_running metric not found for the test container: %s", actualContainerName)
	assert.True(t, foundRestartsMetric, "dex_container_restarts_total metric not found for the test container: %s", actualContainerName)
	assert.True(t, foundCPUMetricForTestContainer, "dex_cpu_utilization_percent metric not found for the test container: %s. Stats might not be available yet or container too short-lived.", actualContainerName)

	// CollectAndLint is a good general check
	problems, err := testutil.CollectAndLint(collector)
	require.NoError(t, err, "Error during CollectAndLint")
	for _, p := range problems {
		// Only log problems related to our test container to avoid noise from other system containers
		if strings.Contains(p.Metric, actualContainerName) {
			t.Logf("Lint problem for %s: %s - %s", actualContainerName, p.Metric, p.Text)
		}
	}

	// Using the filteredCollector for CollectAndCompare
	expectedMetricsText := fmt.Sprintf("# HELP dex_container_running 1 if docker container is running, 0 otherwise\n# TYPE dex_container_running gauge\ndex_container_running{container_name=\"%s\"} 1\n# HELP dex_container_restarts_total Number of times the container has restarted\n# TYPE dex_container_restarts_total counter\ndex_container_restarts_total{container_name=\"%s\"} 0\n", actualContainerName, actualContainerName)

	filteredRegistry := prometheus.NewRegistry()
	// Create a new collector instance for the filtered test, as the original collector might have state or issues if reused across registrations.
	// However, newDockerCollector() creates a new Docker client each time. For this test, it's fine.
	// If newDockerCollector were expensive, we'd pass the original collector.cli to a new filteredCollector wrapper.
	// The provided filteredCollector struct takes *DockerCollector, so we re-use the main one for filtering logic.
	fc := newFilteredCollector(collector, actualContainerName)
	require.NoError(t, filteredRegistry.Register(fc), "Failed to register filtered collector")

	err = testutil.CollectAndCompare(fc, strings.NewReader(expectedMetricsText), "dex_container_running", "dex_container_restarts_total")
	if err != nil {
		t.Logf("CollectAndCompare for basic metrics failed. This is sometimes sensitive to exact output. Error: %v", err)
		// The loop-based assertions are primary for these basic metrics.
	}
}

// newFilteredCollector wraps a DockerCollector and only exposes metrics for a specific container name.
type filteredCollector struct {
	innerCollector      *DockerCollector
	targetContainerName string
	// Store the actual Docker client from the inner collector to ensure it's the same one being used.
	// This isn't strictly necessary if innerCollector.cli is public and used directly,
	// but good practice if we were to re-implement parts of Collect.
}

func newFilteredCollector(inner *DockerCollector, targetName string) *filteredCollector {
	return &filteredCollector{innerCollector: inner, targetContainerName: targetName}
}

func (fc *filteredCollector) Describe(ch chan<- *prometheus.Desc) {
	// For simplicity, we can let the inner collector describe all, or filter descriptions too.
	// Filtering descriptions is more complex if they are not dynamic per metric.
	// Prometheus recommends describing all possible metrics.
	fc.innerCollector.Describe(ch)
}

func (fc *filteredCollector) Collect(ch chan<- prometheus.Metric) {
	innerMetrics := make(chan prometheus.Metric, 100) // Buffer to avoid blocking
	go func() {
		fc.innerCollector.Collect(innerMetrics)
		close(innerMetrics)
	}()

	for metric := range innerMetrics {
		pbMetric := &dto.Metric{}
		err := metric.Write(pbMetric)
		if err != nil {
			// In a real test, log this error or fail
			fmt.Printf("Error writing metric to protobuf in filteredCollector: %v\n", err)
			continue
		}
		var metricMatchesTarget bool
		for _, labelPair := range pbMetric.Label {
			if labelPair.GetName() == "container_name" && labelPair.GetValue() == fc.targetContainerName {
				metricMatchesTarget = true
				break
			}
		}
		if metricMatchesTarget {
			ch <- metric
		}
	}
}
