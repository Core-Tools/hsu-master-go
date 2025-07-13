package domain

type IntegratedUnit struct {
	// Metadata
	Metadata UnitMetadata

	// Discovery
	// Always use process PID file discovery

	// Process control
	Control ManagedProcessControlConfig

	// Health monitoring
	HealthCheckRunOptions HealthCheckRunOptions
}
