package domain

type ManagedUnit struct {
	// Metadata
	Metadata UnitMetadata `yaml:"metadata"`

	// Discovery
	// Always use process PID file discovery

	// Process control
	Control ManagedProcessControlConfig `yaml:"control"`

	// Health monitoring
	HealthCheck HealthCheckConfig `yaml:"health_check,omitempty"`
}
