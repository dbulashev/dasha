package dto

type InvalidConstraint struct {
	Schema           string
	Table            string
	Name             string
	ReferencedSchema string
	ReferencedTable  string
}
