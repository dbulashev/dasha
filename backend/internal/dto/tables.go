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
	SizeIndexes      string
	EstimatedRows    int64
	StatInfo         string
	PartitionOf      string
	Columns          []TableDescribeColumn
	Indexes          []TableDescribeIndex
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
	NullFrac    *float32
	NDistinct   *float32
	AvgWidth    *int32
}

type TableDescribeIndex struct {
	Name       string
	Definition string
	IsPrimary  bool
	IsUnique   bool
	IsValid    bool
	SizeBytes  int64
	Size       string
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

type TableDescribePartition struct {
	Schema              string
	Name                string
	PartitionExpression string
	SizeBytes           int64
	Size                string
}

type TableDescribeBloat struct {
	TableLen             int64
	TableLenPretty       string
	ApproxTupleCount     int64
	ApproxTupleLen       int64
	ApproxTupleLenPretty string
	ApproxTuplePercent   float64
	DeadTupleCount       int64
	DeadTupleLen         int64
	DeadTupleLenPretty   string
	DeadTuplePercent     float64
	ApproxFreeSpace      int64
	ApproxFreeSpacePretty string
	ApproxFreePercent    float64
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
