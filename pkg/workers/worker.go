package workers

import "github.com/core-tools/hsu-master/pkg/workers/processcontrol"

type Worker interface {
	ID() string
	Metadata() UnitMetadata
	ProcessControlOptions() processcontrol.ProcessControlOptions
}
