package workers

import (
	"github.com/core-tools/hsu-master/pkg/monitoring"
	"github.com/core-tools/hsu-master/pkg/process"
	"github.com/core-tools/hsu-master/pkg/workers/processcontrol"
)

type UnmanagedUnit struct {
	// Metadata
	Metadata UnitMetadata `yaml:"metadata"`

	// Discovery
	Discovery process.DiscoveryConfig `yaml:"discovery"`

	// Process control
	Control processcontrol.SystemProcessControlConfig `yaml:"control"`

	// Health monitoring
	HealthCheck monitoring.HealthCheckConfig `yaml:"health_check,omitempty"`
}
