package domain

type UnmanagedUnit struct {
	// Metadata
	Metadata UnitMetadata

	// Discovery
	Discovery DiscoveryConfig

	// Process control
	Control SystemProcessControlConfig

	// Health monitoring
	HealthCheck HealthCheckConfig
}
