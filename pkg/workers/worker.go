package workers

type Worker interface {
	ID() string
	Metadata() UnitMetadata
	ProcessControlOptions() ProcessControlOptions
}
