package domain

type Worker interface {
	ID() string
	Metadata() UnitMetadata
	ProcessControlOptions() ProcessControlOptions
}
