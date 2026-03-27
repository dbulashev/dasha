package dto

type TableTopKBySize struct {
	Table      string
	NIdx       int64
	TotalBytes int64
	Total      string
	Toast      string
	Indexes    string
	Main       string
	Fsm        string
	Vm         string
	StatInfo   string
	Bloat      string
	Options    string
}

type TableCaching struct {
	Schema          string
	Table           string
	HitRate         *float64
	IdxHitRate      *float64
	ToastHitRate    *float64
	ToastIdxHitRate *float64
}

type TableHitRate struct {
	Rate float64
}

type TableDescribe struct {
	Schema      string
	TableName   string
	TableType   string
	AccessMethod string
	Tablespace  string
	Options     string
	SizeTotal   string
	SizeTable   string
	SizeToast   string
	SizeIndexes string
	PartitionOf string
	Columns     []TableDescribeColumn
	Indexes     []TableDescribeIndex
	CheckConstraints []TableDescribeConstraint
	FkConstraints    []TableDescribeConstraint
	ReferencedBy     []TableDescribeReferencedBy
}

type TableDescribeColumn struct {
	Name        string
	Type        string
	Collation   string
	Nullable    bool
	Default     string
	Storage     string
	Description string
}

type TableDescribeIndex struct {
	Name       string
	Definition string
	IsPrimary  bool
	IsUnique   bool
}

type TableDescribeConstraint struct {
	Name       string
	Definition string
}

type TableDescribeReferencedBy struct {
	Name        string
	SourceTable string
	Definition  string
}

type TablePartition struct {
	ParentSchema       string
	Parent             string
	ChildsCount        int64
	ChildsSizeBytes    int64
	ChildsSize         string
	ChildsAvgSizeBytes int64
	ChildsAvgSize      string
}
