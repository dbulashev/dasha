package dto

type FksPossibleNulls struct {
	FkName   string
	RelName  string
	AttNames []string
}

type FksPossibleSimilar struct {
	Table   string
	FkName  string
	Fk1Name string
}

type FkTypeMismatch struct {
	FkName        string
	FromRel       string
	RelAttNames   []string
	ToRel         string
	ToRelAttNames []string
}
