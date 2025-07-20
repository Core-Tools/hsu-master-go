package domain

type UnitMetadata struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
}
