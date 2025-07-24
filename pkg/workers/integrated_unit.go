package workers

import "github.com/core-tools/hsu-master/pkg/monitoring"

type IntegratedUnit struct {
	// Metadata
	Metadata UnitMetadata `yaml:"metadata"`

	// Discovery
	// Always use process PID file discovery

	// Process control
	Control ManagedProcessControlConfig `yaml:"control"`

	// Health monitoring
	HealthCheckRunOptions monitoring.HealthCheckRunOptions `yaml:"health_check_run_options,omitempty"`
}
