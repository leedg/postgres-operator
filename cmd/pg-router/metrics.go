/*
Copyright 2026 keiailab.

Licensed under the MIT License. See the LICENSE file for details.
*/

// metrics.go — pg-router 의 active client-connection 게이지를 Prometheus 텍스트
// exposition 형식으로 노출한다(별 client 의존성 없이 직접 렌더 — 저장소 zero-dep
// 철학). 이 게이지를 custom-metrics adapter(예: prometheus-adapter)가
// custom.metrics.k8s.io 로 매핑하면 router HPA 의 ScaleOnActiveConnections Pods
// 메트릭이 이를 소비한다(RFC 0001 §3.1).
package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/keiailab/postgres-operator/api/v1alpha1"
)

// activeConns 는 현재 진행 중인 client 연결 수다(trackConn 이 inc/dec).
var activeConns atomic.Int64

// trackConn 은 handler 실행 동안 active-connection 게이지를 1 증가시켰다가 복원한다.
// handler 의 panic 여부와 무관하게 defer 로 감소를 보장한다.
func trackConn(handler func()) {
	activeConns.Add(1)
	defer activeConns.Add(-1)
	handler()
}

// writeMetrics 는 Prometheus 텍스트 exposition 을 w 에 쓴다. 게이지 이름은 HPA 가
// 참조하는 v1alpha1.RouterActiveConnectionsMetric 과 동일해 둘이 어긋나지 않는다.
func writeMetrics(w io.Writer) {
	name := v1alpha1.RouterActiveConnectionsMetric
	fmt.Fprintf(w, "# HELP %s Current number of active client connections proxied by pg-router.\n", name)
	fmt.Fprintf(w, "# TYPE %s gauge\n", name)
	fmt.Fprintf(w, "%s %d\n", name, activeConns.Load())
}

// metricsHandler 는 /metrics 응답 핸들러다(테스트에서 httptest 로 직접 호출 가능).
func metricsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		writeMetrics(w)
	}
}

// serveMetrics 는 addr 에서 /metrics + /healthz HTTP 서버를 띄운다(블로킹 —
// goroutine 으로 호출). addr 이 빈 문자열이면 no-op(비활성).
func serveMetrics(addr string) {
	if addr == "" {
		return
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", metricsHandler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("pg-router metrics listening on %s (/metrics, /healthz)", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("pg-router: metrics server: %v", err)
	}
}
