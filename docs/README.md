# DEX - Docker EXporter for prometheus

DEX is a lightweight Prometheus exporter that monitors Docker containers and exports their metrics. It's designed to run as a Docker container and supports all architectures.

## Key Features
- Lightweight and efficient
- Supports all architectures
- Real-time container metrics monitoring
- Easy integration with Prometheus and Grafana
- No configuration required for basic usage

## Exposed Metrics

| Metric Name | Type | Description |
|------------|------|-------------|
| dex_block_io_read_bytes_total | Counter | Total number of bytes read from block devices |
| dex_block_io_write_bytes_total | Counter | Total number of bytes written to block devices |
| dex_container_exited | Gauge | 1 if container has exited, 0 otherwise |
| dex_container_restarting | Gauge | 1 if container is restarting, 0 otherwise |
| dex_container_restarts_total | Counter | Total number of container restarts |
| dex_container_running | Gauge | 1 if container is running, 0 otherwise |
| dex_cpu_utilization_percent | Gauge | Current CPU utilization percentage |
| dex_cpu_utilization_seconds_total | Counter | Cumulative CPU time consumed |
| dex_memory_total_bytes | Gauge | Total memory limit in bytes |
| dex_memory_usage_bytes | Counter | Current memory usage in bytes |
| dex_memory_utilization_percent | Gauge | Current memory utilization percentage |
| dex_network_rx_bytes_total | Counter | Total bytes received over network |
| dex_network_tx_bytes_total | Counter | Total bytes transmitted over network |
| dex_pids_current | Counter | Current number of processes in the container |

## Prerequisites
- Docker installed and running
- Prometheus server (for metrics collection)
- Grafana (optional, for visualization)

## Run with docker
Start docker container with following `docker-compose.yml`:
```yml
version: '2.1'
services:
   dex:
      image: spx01/dex
      container_name: dex
      volumes:
         - /var/run/docker.sock:/var/run/docker.sock
      ports:
         - 8386:8080
      restart: always
```

## Test with curl
```
$ curl localhost:8386/metrics
```

## Grafana dashboard

### Grafana 7

Example grafana7 dashboard definition [as JSON](grafana7.json)
![grafana-dashboard](grafana7-dashboard.png).

### Grafana 8

Another dashboard for Grafana 8 (thanks @scMarkus !!!) [as JSON](grafana8.json)
![grafana-dashboard](grafana8-dashboard.png)

Modification (thanks @GitSchorsch) with additional job filter [as JSON](grafana8_2.json)
