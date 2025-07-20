package domain

type IntegratedUnit struct {
	// Metadata
	Metadata UnitMetadata `yaml:"metadata"`

	// Discovery
	// Always use process PID file discovery

	// Process control
	Control ManagedProcessControlConfig `yaml:"control"`

	// Health monitoring
	HealthCheckRunOptions HealthCheckRunOptions `yaml:"health_check_run_options,omitempty"`
}
