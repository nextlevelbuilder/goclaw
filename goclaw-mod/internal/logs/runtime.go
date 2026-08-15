package logs

// RuntimeAggregateOpts configures aggregation over the in-memory runtime log ring.
type RuntimeAggregateOpts struct {
	GroupBy string
	Level   string
	Source  string
	FromMS  int64
}

// RuntimeAggregateBucket is a grouped runtime log count.
type RuntimeAggregateBucket struct {
	Key      string `json:"key"`
	Count    int    `json:"count"`
	LastSeen int64  `json:"last_seen"`
}

// RuntimeAggregateResult describes the bounded runtime log aggregate.
type RuntimeAggregateResult struct {
	Source     string                   `json:"source"`
	Retention  string                   `json:"retention"`
	Capacity   int                      `json:"capacity"`
	SampleSize int                      `json:"sample_size"`
	GroupBy    string                   `json:"group_by"`
	Buckets    []RuntimeAggregateBucket `json:"buckets"`
}
