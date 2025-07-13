package domain

type ManagedUnit struct {
	// Metadata
	Metadata UnitMetadata

	// Discovery
	// Always use process PID file discovery

	// Process control
	Control ManagedProcessControl

	// Health monitoring
	HealthCheck GenericHealthCheckConfig
}
