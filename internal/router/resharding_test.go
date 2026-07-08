/*
Copyright 2026 keiailab.

Licensed under the MIT License. See the LICENSE file for details.
*/

package router

import (
	"errors"
	"testing"

	"github.com/keiailab/postgres-operator/api/v1alpha1"
)

func entry(lo, hi, shard string) v1alpha1.ShardRangeEntry {
	return v1alpha1.ShardRangeEntry{Lo: lo, Hi: hi, Shard: shard}
}

func TestSplitHashRange(t *testing.T) {
	tests := []struct {
		name                   string
		lo, hi                 string
		wantLo0, wantHi0       string
		wantLo1, wantHi1       string
		wantErr                error
	}{
		{
			name: "full range halves",
			lo:   "0x00000000", hi: "0xffffffff",
			wantLo0: "0x00000000", wantHi0: "0x7fffffff",
			wantLo1: "0x80000000", wantHi1: "0xffffffff",
		},
		{
			name: "lower half of a prior split",
			lo:   "0x00000000", hi: "0x7fffffff",
			wantLo0: "0x00000000", wantHi0: "0x3fffffff",
			wantLo1: "0x40000000", wantHi1: "0x7fffffff",
		},
		{
			name: "two-key range splits into singletons",
			lo:   "0x00000010", hi: "0x00000011",
			wantLo0: "0x00000010", wantHi0: "0x00000010",
			wantLo1: "0x00000011", wantHi1: "0x00000011",
		},
		{
			name: "single-key range cannot split",
			lo:   "0x00000005", hi: "0x00000005",
			wantErr: ErrHashRangeTooSmall,
		},
		{
			name: "lo greater than hi is rejected",
			lo:   "0x0000000a", hi: "0x00000005",
			wantErr: errors.New("lo > hi"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lo0, hi0, lo1, hi1, err := SplitHashRange(tc.lo, tc.hi)
			if tc.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error, got nil (%s,%s,%s,%s)", lo0, hi0, lo1, hi1)
				}
				if errors.Is(tc.wantErr, ErrHashRangeTooSmall) && !errors.Is(err, ErrHashRangeTooSmall) {
					t.Fatalf("want ErrHashRangeTooSmall, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if lo0 != tc.wantLo0 || hi0 != tc.wantHi0 || lo1 != tc.wantLo1 || hi1 != tc.wantHi1 {
				t.Fatalf("split = [%s,%s],[%s,%s], want [%s,%s],[%s,%s]",
					lo0, hi0, lo1, hi1, tc.wantLo0, tc.wantHi0, tc.wantLo1, tc.wantHi1)
			}
			// 데이터 보존 불변식: 두 하위 범위의 합집합 = 원본, gap/overlap 0.
			targets := []v1alpha1.ShardRangeEntry{
				{Lo: lo0, Hi: hi0, Shard: "a"},
				{Lo: lo1, Hi: hi1, Shard: "b"},
			}
			source := []v1alpha1.ShardRangeEntry{{Lo: tc.lo, Hi: tc.hi, Shard: "src"}}
			if err := ValidateSplitPlan(source, targets); err != nil {
				t.Fatalf("split violates preservation invariant: %v", err)
			}
		})
	}
}

func TestValidateSplitPlan(t *testing.T) {
	full := []v1alpha1.ShardRangeEntry{entry("0x00000000", "0xffffffff", "s0")}

	cases := []struct {
		name    string
		sources []v1alpha1.ShardRangeEntry
		targets []v1alpha1.ShardRangeEntry
		wantErr error
	}{
		{
			name:    "valid 2-way split (data 보존)",
			sources: full,
			targets: []v1alpha1.ShardRangeEntry{
				entry("0x00000000", "0x7fffffff", "t0"),
				entry("0x80000000", "0xffffffff", "t1"),
			},
			wantErr: nil,
		},
		{
			name:    "overlap (키 중복)",
			sources: full,
			targets: []v1alpha1.ShardRangeEntry{
				entry("0x00000000", "0x80000000", "t0"),
				entry("0x7fffffff", "0xffffffff", "t1"),
			},
			wantErr: ErrSplitPlanOverlap,
		},
		{
			name:    "gap (키 유실)",
			sources: full,
			targets: []v1alpha1.ShardRangeEntry{
				entry("0x00000000", "0x7ffffffe", "t0"),
				entry("0x80000000", "0xffffffff", "t1"),
			},
			wantErr: ErrSplitPlanGap,
		},
		{
			name:    "coverage mismatch (target 이 source 보다 좁음)",
			sources: full,
			targets: []v1alpha1.ShardRangeEntry{
				entry("0x00000000", "0x7fffffff", "t0"),
			},
			wantErr: ErrSplitPlanCoverage,
		},
		{
			name: "valid merge (역방향: 2 → 1)",
			sources: []v1alpha1.ShardRangeEntry{
				entry("0x00000000", "0x7fffffff", "s0"),
				entry("0x80000000", "0xffffffff", "s1"),
			},
			targets: full,
			wantErr: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSplitPlan(tc.sources, tc.targets)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("ValidateSplitPlan = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("ValidateSplitPlan = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestHexSuccessor(t *testing.T) {
	cases := map[string]string{
		"0x00000000": "0x00000001",
		"0x00000009": "0x0000000a",
		"0x0000000f": "0x00000010",
		"0x7fffffff": "0x80000000",
		"0xfffffffe": "0xffffffff",
	}
	for in, want := range cases {
		if got := hexSuccessor(in); got != want {
			t.Errorf("hexSuccessor(%s) = %s, want %s", in, got, want)
		}
	}
}
