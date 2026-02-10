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

type TablePartition struct {
	ParentSchema       string
	Parent             string
	ChildsCount        int64
	ChildsSizeBytes    int64
	ChildsSize         string
	ChildsAvgSizeBytes int64
	ChildsAvgSize      string
}
