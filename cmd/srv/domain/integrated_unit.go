package domain

type IntegratedUnit struct {
	// Metadata
	Metadata UnitMetadata

	// Discovery
	// Always use process PID file discovery

	// Process control
	Control ManagedProcessControl

	// Health monitoring
	HealthCheckRunOptions HealthCheckRunOptions
}
