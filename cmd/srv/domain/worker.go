package domain

type Worker interface {
	ID() string
	Metadata() UnitMetadata
	ProcessControl() GenericProcessControl
	HealthCheckConfig() GenericHealthCheckConfig
	DiscoveryConfig() GenericDiscoveryConfig
}
