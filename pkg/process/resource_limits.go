package process

type ResourceLimits struct {
	// Process priority
	Priority int `yaml:"priority,omitempty"`

	// CPU limits
	CPU       float64 `yaml:"cpu,omitempty"`        // Number of CPU cores
	CPUShares int     `yaml:"cpu_shares,omitempty"` // CPU weight (Linux cgroups)

	// Memory limits
	Memory     int64 `yaml:"memory,omitempty"`      // Memory limit in bytes
	MemorySwap int64 `yaml:"memory_swap,omitempty"` // Memory + swap limit

	// Process limits
	MaxProcesses int `yaml:"max_processes,omitempty"`  // Maximum number of processes
	MaxOpenFiles int `yaml:"max_open_files,omitempty"` // Maximum open file descriptors

	// I/O limits
	IOWeight   int   `yaml:"io_weight,omitempty"`    // I/O priority weight
	IOReadBPS  int64 `yaml:"io_read_bps,omitempty"`  // Read bandwidth limit
	IOWriteBPS int64 `yaml:"io_write_bps,omitempty"` // Write bandwidth limit
}
