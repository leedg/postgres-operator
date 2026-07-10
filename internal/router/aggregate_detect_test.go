/*
Copyright 2026 keiailab.

Licensed under the MIT License. See the LICENSE file for details.
*/

package router

import (
	"context"
	"reflect"
	"testing"
)

func TestDetectAggregates(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  []AggregateFunc
		ok    bool
	}{
		{"scalar count", "SELECT count(*) FROM t", []AggregateFunc{AggCount}, true},
		{"count + sum", "SELECT count(*), sum(x) FROM t", []AggregateFunc{AggCount, AggSum}, true},
		{"min + max", "SELECT min(x), max(y) FROM t", []AggregateFunc{AggMin, AggMax}, true},
		{"group by", "SELECT region, count(*) FROM t GROUP BY region", []AggregateFunc{AggNone, AggCount}, true},
		{"group by two keys", "SELECT a, b, sum(v) FROM t GROUP BY a, b", []AggregateFunc{AggNone, AggNone, AggSum}, true},
		{"alias", "SELECT count(*) AS c FROM t", []AggregateFunc{AggCount}, true},
		{"alias no as", "SELECT count(*) c FROM t", []AggregateFunc{AggCount}, true},
		{"qualified arg", "SELECT count(a.id) FROM t a", []AggregateFunc{AggCount}, true},
		{"case-insensitive", "select COUNT(*), Sum(x) from t", []AggregateFunc{AggCount, AggSum}, true},

		// 대상 아님 / 재결합 불가 → ok=false.
		{"no aggregate", "SELECT x, y FROM t", nil, false},
		{"avg unsupported", "SELECT avg(x) FROM t", nil, false},
		{"avg mixed", "SELECT count(*), avg(x) FROM t", nil, false},
		{"expression with aggregate", "SELECT 1 + count(*) FROM t", nil, false},
		{"arithmetic after agg", "SELECT sum(x) + 1 FROM t", nil, false},
		{"select star", "SELECT * FROM t", nil, false},
		{"multi statement", "SELECT count(*) FROM t; SELECT 1", nil, false},
		{"distinct", "SELECT DISTINCT x FROM t", nil, false},
		{"not select", "UPDATE t SET x = 1", nil, false},
		{"no from", "SELECT count(*)", nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := DetectAggregates(tc.query)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v (got %v)", ok, tc.ok, got)
			}
			if tc.ok && !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("aggs = %v, want %v", got, tc.want)
			}
		})
	}
}

// 감지 결과가 실제 merge 와 정합: count+sum 감지 → MergeAggregate 로 재결합 시 정답.
func TestDetectAggregates_EndToEndWithMerge(t *testing.T) {
	q := "SELECT region, count(*), sum(amount) FROM sales GROUP BY region"
	aggs, ok := DetectAggregates(q)
	if !ok {
		t.Fatal("expected aggregate query")
	}
	sg := NewScatterGather()
	sg.Merge = MergeAggregate
	sg.Aggregates = aggs
	sg.Shard = &fakeShardExecutor{responses: map[ShardID][]Row{
		"s-0": {{Values: []any{"us", int64(3), int64(300)}}},
		"s-1": {{Values: []any{"us", int64(2), int64(200)}}, {Values: []any{"eu", int64(1), int64(50)}}},
	}}
	rows, err := sg.Execute(context.Background(), q, []ShardID{"s-0", "s-1"})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	got := map[string][2]int64{}
	for _, r := range rows {
		got[r.Values[0].(string)] = [2]int64{r.Values[1].(int64), r.Values[2].(int64)}
	}
	if got["us"] != [2]int64{5, 500} || got["eu"] != [2]int64{1, 50} {
		t.Fatalf("merged = %v, want us{5,500} eu{1,50}", got)
	}
}
