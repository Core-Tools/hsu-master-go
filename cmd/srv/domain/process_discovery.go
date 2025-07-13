package domain

import "time"

type DiscoveryMethod string

const (
	DiscoveryMethodProcessName DiscoveryMethod = "process-name"
	DiscoveryMethodPort        DiscoveryMethod = "port"
	DiscoveryMethodPIDFile     DiscoveryMethod = "pid-file"
	DiscoveryMethodServiceName DiscoveryMethod = "service-name"
)

type GenericDiscoveryConfig struct {
	Method DiscoveryMethod

	// Process name discovery
	ProcessName string
	ProcessArgs []string // Optional: match by command line args

	// Port discovery
	Port     int
	Protocol string // "tcp", "udp"

	// PID file discovery
	PIDFile string

	// Service discovery (systemd, Windows services)
	ServiceName string

	// Discovery frequency
	CheckInterval time.Duration
}
