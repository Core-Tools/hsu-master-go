package domain

type UnmanagedUnit struct {
	// Metadata
	Metadata UnitMetadata `yaml:"metadata"`

	// Discovery
	Discovery DiscoveryConfig `yaml:"discovery"`

	// Process control
	Control SystemProcessControlConfig `yaml:"control"`

	// Health monitoring
	HealthCheck HealthCheckConfig `yaml:"health_check,omitempty"`
}
