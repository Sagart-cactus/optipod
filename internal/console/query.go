package console

import (
	"context"
	"fmt"
	"strconv"
)

func (s *Store) Clusters(ctx context.Context, allowed []string) ([]Cluster, error) {
	rows, err := s.Pool.Query(ctx, `SELECT id,name,kube_uid,last_seq,captured_at,received_at,warnings FROM op_clusters WHERE id=ANY($1) ORDER BY name,id`, allowed)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Cluster{}
	for rows.Next() {
		var c Cluster
		if err := rows.Scan(&c.ID, &c.Name, &c.UID, &c.LastSequence, &c.CapturedAt, &c.ReceivedAt, &c.Warnings); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// List uses keyset pagination and an explicit allow-list of queryable tables.
// Authorization is part of SQL, never a filter applied after pagination.
func (s *Store) List(ctx context.Context, kind string, q Query) (*Page, error) {
	tables := map[string]string{resourceWorkloads: "op_workloads", resourcePolicies: "op_policies", "revisions": "op_policy_revisions", "history": "op_history", "samples": "op_samples"}
	table, ok := tables[kind]
	if !ok {
		return nil, fmt.Errorf("unknown resource")
	}
	if q.Limit < 1 || q.Limit > 200 {
		q.Limit = 50
	}
	var cursor int64
	var err error
	if q.Cursor != "" {
		cursor, err = strconv.ParseInt(q.Cursor, 10, 64)
		if err != nil || cursor < 1 {
			return nil, fmt.Errorf("invalid cursor")
		}
	}
	deleted := "false"
	filter := ""
	if kind == resourceWorkloads || kind == resourcePolicies {
		deleted = "deleted"
		if !q.IncludeDeleted {
			filter += " AND NOT deleted"
		}
	}
	args := []any{q.Clusters, q.Cluster, q.Namespace, q.UID, q.Search, cursor, q.Limit + 1, q.Namespaces}
	if kind == resourceWorkloads && q.Mode != "" {
		args = append(args, q.Mode)
		filter += " AND mode=$9"
	}
	rows, err := s.Pool.Query(ctx, `SELECT id,cluster_id,observed_at,`+deleted+`,data FROM `+table+`
	 WHERE cluster_id=ANY($1) AND ($2='' OR cluster_id=$2) AND ($3='' OR namespace=$3)
	 AND ($4='' OR uid=$4) AND ($5='' OR strpos(lower(name||' '||namespace),lower($5))>0)
	 AND ($6::bigint=0 OR id<$6) AND (COALESCE(cardinality($8::text[]),0)=0 OR namespace=ANY($8)) `+filter+` ORDER BY id DESC LIMIT $7`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	p := &Page{Items: []Item{}}
	for rows.Next() {
		var i Item
		if err := rows.Scan(&i.ID, &i.ClusterID, &i.ObservedAt, &i.Deleted, &i.Data); err != nil {
			return nil, err
		}
		p.Items = append(p.Items, i)
	}
	if len(p.Items) > q.Limit {
		p.Items = p.Items[:q.Limit]
		p.NextCursor = strconv.FormatInt(p.Items[len(p.Items)-1].ID, 10)
	}
	return p, rows.Err()
}
