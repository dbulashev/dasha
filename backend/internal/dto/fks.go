package dto

type FksPossibleNulls struct {
	Schema   string
	FkName   string
	RelName  string
	AttNames []string
}

type FksPossibleSimilar struct {
	Schema  string
	Table   string
	FkName  string
	Fk1Name string
}

type FkTypeMismatch struct {
	Schema        string
	FkName        string
	FromRel       string
	RelAttNames   []string
	ToSchema      string
	ToRel         string
	ToRelAttNames []string
}
