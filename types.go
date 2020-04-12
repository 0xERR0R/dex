package main

type containerStats struct {
	Id      string `json:"id"`
	Read    string `json:"read"`
	BlockIO struct {
		IOBytes []IOService `json:"io_service_bytes_recursive"`
	} `json:"blkio_stats"`
	CPU struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemCpuUsage uint64 `json:"system_cpu_usage"`
	} `json:"cpu_stats"`
	PreCPU struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemCpuUsage uint64 `json:"system_cpu_usage"`
	} `json:"precpu_stats"`
	Memory struct {
		Usage       uint64 `json:"usage"`
		Limit       uint64 `json:"limit"`
		MemoryStats struct {
			Cache uint64 `json:"cache"`
		} `json:"stats"`
	} `json:"memory_stats"`
	Networks struct {
		Eth0 struct {
			RxBytes uint64 `json:"rx_bytes"`
			TxBytes uint64 `json:"tx_bytes"`
		} `json:"eth0"`
	} `json:"networks"`
}

type IOService struct {
	Op    string `json:"op"`
	Value uint64 `json:"value"`
}
