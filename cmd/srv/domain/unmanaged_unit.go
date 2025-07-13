package domain

type UnmanagedUnit struct {
	// Metadata
	Metadata UnitMetadata

	// Discovery
	Discovery GenericDiscoveryConfig

	// Process control
	Control SystemProcessControl

	// Health monitoring
	HealthCheck GenericHealthCheckConfig
}
