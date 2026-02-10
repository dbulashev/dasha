package dto

type IndexBloat struct {
	Schema     string
	Table      string
	Index      string
	BloatBytes int64
	IndexBytes int64
	Definition string
	Primary    bool
}

type IndexBtreeOnArray struct {
	Table string
	Index string
}

type IndexCaching struct {
	Schema  string
	Table   string
	Index   string
	HitRate float64
}

type IndexHitRate struct {
	Rate float64
}

type IndexInvalidOrNotReady struct {
	Table      string
	IndexName  string
	IsValid    bool
	IsReady    bool
	Constraint string
}

type IndexMissing struct {
	Schema                  string
	Table                   string
	PercentOfTimesIndexUsed *float64
	EstimatedRows           int64
}

type IndexSimilar1 struct {
	Table                   string
	I1UniqueIndexName       string
	I2IndexName             string
	I1UniqueIndexDefinition string
	I2IndexDefinition       string
	I1UsedInConstraint      string
	I2UsedInConstraint      string
}

type IndexSimilar2 struct {
	Table   string
	FkName  string
	FkName2 string
}

type IndexSimilar3 struct {
	Table                     string
	I1IndexName               string
	I2IndexName               string
	SimplifiedIndexDefinition string
	I1IndexDefinition         string
	I2IndexDefinition         string
	I1UsedInConstraint        string
	I2UsedInConstraint        string
}

type IndexTopKBySize struct {
	Tablespace string
	Table      string
	Index      string
	Size       string
	SizeBytes  int64
}

type IndexUnused struct {
	Schema     string
	Table      string
	Index      string
	SizeBytes  int64
	IndexScans int64
}

type IndexUsage struct {
	Schema                  string
	Table                   string
	PercentOfTimesIndexUsed *float64
	EstimatedRows           int64
}
