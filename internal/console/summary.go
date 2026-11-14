package console

import "context"

type Summary struct {
	Workloads            int64 `json:"workloads"`
	Policies             int64 `json:"policies"`
	Records              int64 `json:"records"`
	RequestedMilliCPU    int64 `json:"requestedMilliCPU"`
	RequestedMemoryBytes int64 `json:"requestedMemoryBytes"`
}

func (s *Store) Summary(ctx context.Context, reader *Reader, cluster string) (*Summary, error) {
	result := &Summary{}
	args := []any{reader.Clusters, reader.Namespaces, cluster}
	scope := ` WHERE cluster_id=ANY($1) AND (COALESCE(cardinality($2::text[]),0)=0 OR namespace=ANY($2)) AND ($3='' OR cluster_id=$3)`
	err := s.Pool.QueryRow(ctx, `SELECT count(*),COALESCE(sum((data->>'requestedMilliCPU')::bigint),0),COALESCE(sum((data->>'requestedMemoryBytes')::bigint),0) FROM op_workloads`+scope+` AND NOT deleted`, args...).Scan(&result.Workloads, &result.RequestedMilliCPU, &result.RequestedMemoryBytes)
	if err != nil {
		return nil, err
	}
	if err = s.Pool.QueryRow(ctx, `SELECT count(*) FROM op_policies`+scope+` AND NOT deleted`, args...).Scan(&result.Policies); err != nil {
		return nil, err
	}
	if err = s.Pool.QueryRow(ctx, `SELECT count(*) FROM op_history`+scope, args...).Scan(&result.Records); err != nil {
		return nil, err
	}
	return result, nil
}
