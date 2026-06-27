/*
Copyright 2026 keiailab.

Licensed under the MIT License. See the LICENSE file for details.
*/

// Command router-bench 는 pg-router 의 *분산 처리 능력*(워커 수 × TPS)과 라우팅 오버헤드를
// 실측한다. internal/router.ResolveShard(라우터와 동일한 vindex)로 키를 샤드에 배치한 뒤,
// 점(point) 읽기/쓰기를 워커 수를 늘려가며 고정 시간 동안 던져 처리량을 잰다.
//
// 시나리오:
//   - direct-shard0 : 라우터 없이 shard-0 에 직접(기준선) — 라우터 오버헤드 분리용.
//   - router-1shard : 라우터 경유, shard-0 키만(단일샤드 처리량).
//   - router-2shard : 라우터 경유, 전 키스페이스(2샤드 분산 처리량).
//
// 한 호스트에서 샤드들과 라우터가 CPU 를 공유하므로 선형 스케일은 기대하지 않는다 —
// 수치는 그 환경 기준의 상대 비교로 해석할 것(docs/perf/baseline.md).
//
// 환경변수: BENCH_ROUTER, BENCH_SHARD0, BENCH_SHARD1 (lib/pq DSN), BENCH_KEYS(10000),
// BENCH_DURATION(5s), BENCH_WORKERS(1,2,4,8,16,32), BENCH_MODE(select|update).
package main

import (
	"database/sql"
	"fmt"
	"log"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/lib/pq"

	"github.com/keiailab/postgres-operator/api/v1alpha1"
	"github.com/keiailab/postgres-operator/internal/router"
)

// benchSpec 는 pg-router 의 기본 static 토폴로지(cmd/pg-router/main.go shardSpec)와
// *동일* 해야 한다 — 데이터 배치를 라우팅과 일치시키기 위함.
func benchSpec() v1alpha1.ShardRangeSpec {
	return v1alpha1.ShardRangeSpec{
		Vindex: v1alpha1.VindexSpec{Type: v1alpha1.VindexTypeHash, Column: "id", Function: "murmur3"},
		Ranges: []v1alpha1.ShardRangeEntry{
			{Lo: "0x00000000", Hi: "0x7fffffff", Shard: "shard-0"},
			{Lo: "0x80000000", Hi: "0xffffffff", Shard: "shard-1"},
		},
	}
}

func main() {
	var (
		routerDSN = env("BENCH_ROUTER", "host=pgrouter port=5432 user=postgres dbname=postgres sslmode=disable")
		shard0DSN = env("BENCH_SHARD0", "host=pg-shard-0 port=5432 user=postgres password=secret dbname=postgres sslmode=disable")
		shard1DSN = env("BENCH_SHARD1", "host=pg-shard-1 port=5432 user=postgres password=secret dbname=postgres sslmode=disable")
		keys      = envInt("BENCH_KEYS", 10000)
		dur       = envDur("BENCH_DURATION", 5*time.Second)
		workers   = envInts("BENCH_WORKERS", []int{1, 2, 4, 8, 16, 32})
		mode      = env("BENCH_MODE", "select")
	)

	spec := benchSpec()
	keysOnShard0 := seed(shard0DSN, shard1DSN, spec, keys)
	log.Printf("seeded %d keys: shard-0=%d shard-1=%d (mode=%s dur=%s)", keys, len(keysOnShard0), keys-len(keysOnShard0), mode, dur)

	allKeys := make([]int, keys)
	for i := range allKeys {
		allKeys[i] = i + 1
	}

	scenarios := []struct {
		name string
		dsn  string
		keys []int
	}{
		{"direct-shard0", shard0DSN, keysOnShard0},
		{"router-1shard", routerDSN, keysOnShard0},
		{"router-2shard", routerDSN, allKeys},
	}

	fmt.Printf("\n%-16s %8s %12s %12s %10s\n", "scenario", "workers", "ops", "TPS", "avg_ms")
	fmt.Println(strings.Repeat("-", 62))
	for _, sc := range scenarios {
		for _, w := range workers {
			ops, avgMs := run(sc.dsn, sc.keys, w, dur, mode)
			tps := float64(ops) / dur.Seconds()
			fmt.Printf("%-16s %8d %12d %12.0f %10.3f\n", sc.name, w, ops, tps, avgMs)
		}
		fmt.Println()
	}
}

// seed 는 두 샤드에 kv 테이블을 만들고, 라우터와 동일한 vindex 로 각 키를 해당 샤드에
// 직접 적재한다. shard-0 에 놓인 키 목록을 반환한다(단일샤드 시나리오용).
func seed(shard0DSN, shard1DSN string, spec v1alpha1.ShardRangeSpec, keys int) []int {
	db0 := mustOpen(shard0DSN)
	db1 := mustOpen(shard1DSN)
	defer db0.Close()
	defer db1.Close()
	for _, db := range []*sql.DB{db0, db1} {
		mustExec(db, `DROP TABLE IF EXISTS kv`)
		mustExec(db, `CREATE TABLE kv (id int PRIMARY KEY, val int)`)
	}
	txt := map[string]*sql.Tx{"shard-0": mustBegin(db0), "shard-1": mustBegin(db1)}
	var onShard0 []int
	for id := 1; id <= keys; id++ {
		shard, err := router.ResolveShard(spec, strconv.Itoa(id))
		if err != nil {
			log.Fatalf("resolve %d: %v", id, err)
		}
		if _, err := txt[shard].Exec(`INSERT INTO kv(id,val) VALUES($1,$2)`, id, id*10); err != nil {
			log.Fatalf("insert %d -> %s: %v", id, shard, err)
		}
		if shard == "shard-0" {
			onShard0 = append(onShard0, id)
		}
	}
	for _, tx := range txt {
		if err := tx.Commit(); err != nil {
			log.Fatal("commit: ", err)
		}
	}
	return onShard0
}

// run 은 db 에 w 개 워커로 dur 동안 점 쿼리를 던지고 (총 ops, 평균 지연ms)를 반환한다.
func run(dsn string, keys []int, w int, dur time.Duration, mode string) (int64, float64) {
	db := mustOpen(dsn)
	defer db.Close()
	db.SetMaxOpenConns(w)
	db.SetMaxIdleConns(w)

	query := `SELECT val FROM kv WHERE id=$1`
	if mode == "update" {
		query = `UPDATE kv SET val=val+1 WHERE id=$1`
	}

	var ops int64
	var totalNs int64
	deadline := time.Now().Add(dur)
	var wg sync.WaitGroup
	for i := 0; i < w; i++ {
		wg.Add(1)
		go func(seed int64) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(seed))
			for time.Now().Before(deadline) {
				id := keys[rng.Intn(len(keys))]
				t0 := time.Now()
				var err error
				if mode == "update" {
					_, err = db.Exec(query, id)
				} else {
					var val int
					err = db.QueryRow(query, id).Scan(&val)
				}
				if err != nil {
					log.Printf("query err: %v", err)
					return
				}
				atomic.AddInt64(&totalNs, time.Since(t0).Nanoseconds())
				atomic.AddInt64(&ops, 1)
			}
		}(int64(i) + 1)
	}
	wg.Wait()
	avgMs := 0.0
	if ops > 0 {
		avgMs = float64(totalNs) / float64(ops) / 1e6
	}
	return ops, avgMs
}

func mustOpen(dsn string) *sql.DB {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("open %q: %v", dsn, err)
	}
	return db
}

func mustExec(db *sql.DB, q string) {
	if _, err := db.Exec(q); err != nil {
		log.Fatalf("exec %q: %v", q, err)
	}
}

func mustBegin(db *sql.DB) *sql.Tx {
	tx, err := db.Begin()
	if err != nil {
		log.Fatal("begin: ", err)
	}
	return tx
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envDur(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func envInts(k string, def []int) []int {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	var out []int
	for _, p := range strings.Split(v, ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(p)); err == nil {
			out = append(out, n)
		}
	}
	if len(out) == 0 {
		return def
	}
	sort.Ints(out)
	return out
}
