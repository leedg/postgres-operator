/*
Copyright 2026 keiailab.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Package main은 keiailab/postgres-operator의 instance manager 바이너리다.
//
// 본 바이너리는 PG Pod의 PID 1로 동작하여 postgres 자식 프로세스를 supervise
// 한다(ADR 0002). 외부 DCS(etcd/Consul) 없이 K8s API의 lease 객체를 합의
// 메커니즘으로 사용하며, 모든 PG 인스턴스(coordinator/worker)에서 동일 코드가
// 동작한다.
//
// 본 파일은 Pillar P1-T3의 골격이다. 현재는 다음만 보유한다:
//   - 신호 처리(SIGTERM/SIGINT graceful shutdown)
//   - HTTP healthz/readyz 엔드포인트
//   - 구조화 로깅
//
// 후속 task에서 추가될 책임:
//   - postgres 자식 프로세스 fork + 감독 + restart 정책 (P1-T3 보강)
//   - K8s lease 기반 election 참여 (P2-T1, internal/instance/election/)
//   - PVC fencing 검사 (P2-T2)
//   - pg_rewind 자동화 (P2-T4)
//   - failover 시 citus_update_node 호출 (P11-T8, internal/instance/citus/)
//
// 빌드:
//
//	docker buildx build --builder masblue-builder \
//	  -t ghcr.io/keiailab/postgres-operator-instance:<rev> \
//	  -f build/images/instance/Dockerfile .
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	var probeAddr string
	flag.StringVar(&probeAddr, "probe-bind-address", ":8080",
		"The address the healthz/readyz endpoints bind to.")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	logger.Info("Instance manager starting",
		"version", "v0.0.0-pillar-p1-t3-skeleton",
		"probeAddr", probeAddr,
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		// 현재는 프로세스 생존만 보고. P1-T3 후속에서 postgres 자식 PID alive
		// 검사로 보강.
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		// P1-T3 후속에서 postgres가 SQL 쿼리에 응답 가능한지 검사.
		// 또한 P12 라우터 사이드카에 한해 router_metadata_lag_seconds 임계
		// 초과 시 503 반환(ADR 0003).
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:              probeAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// 종료 신호를 처리할 컨텍스트.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	srvErrCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			srvErrCh <- err
		}
		close(srvErrCh)
	}()

	logger.Info("Probe endpoints listening", "addr", probeAddr)

	select {
	case <-ctx.Done():
		logger.Info("Received shutdown signal")
	case err := <-srvErrCh:
		logger.Error("Probe server failed", "error", err)
		os.Exit(1)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("Probe server graceful shutdown failed", "error", err)
		os.Exit(1)
	}
	logger.Info("Instance manager exited cleanly")
}
