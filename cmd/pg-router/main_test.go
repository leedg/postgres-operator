/*
Copyright 2026 keiailab.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package main

import (
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/keiailab/postgres-operator/internal/router"
)

func TestReadStartupParsesParams(t *testing.T) {
	t.Parallel()

	paramBytes := []byte("user\x00alice\x00database\x00shop\x00\x00")
	body := make([]byte, 4+len(paramBytes))
	binary.BigEndian.PutUint32(body[0:4], 196608) // protocol v3.0
	copy(body[4:], paramBytes)
	msg := make([]byte, 4+len(body))
	binary.BigEndian.PutUint32(msg[0:4], uint32(4+len(body)))
	copy(msg[4:], body)

	c1, c2 := net.Pipe()
	defer func() { _ = c1.Close() }()
	go func() { _, _ = c2.Write(msg); _ = c2.Close() }()

	_ = c1.SetReadDeadline(time.Now().Add(2 * time.Second))
	raw, params, err := readStartup(c1)
	if err != nil {
		t.Fatalf("readStartup: %v", err)
	}
	if params["user"] != "alice" || params["database"] != "shop" {
		t.Fatalf("params = %v, want user=alice database=shop", params)
	}
	if len(raw) != len(msg) {
		t.Fatalf("raw len %d != original %d (must be forwardable verbatim)", len(raw), len(msg))
	}
}

func TestShardSpecRoutesByVindex(t *testing.T) {
	t.Parallel()

	spec := shardSpec()
	seen := map[string]bool{}
	for _, key := range []string{"alice", "bob", "carol", "dave", "eve", "frank", "grace", "heidi", "ivan", "judy"} {
		sh, err := router.ResolveShard(spec, key)
		if err != nil {
			t.Fatalf("ResolveShard(%q): %v", key, err)
		}
		if sh != "shard-0" && sh != "shard-1" {
			t.Fatalf("key %q -> unexpected shard %q", key, sh)
		}
		seen[sh] = true
	}
	// The PoC's whole point: the vindex is a live consumer and every key maps to
	// a real shard. (Distribution across both shards is best-effort with a small
	// sample, so we only log if it lands on one.)
	if len(seen) < 2 {
		t.Logf("note: sample keys all hashed to %v", seen)
	}
}

func TestBackendForUsesEnvMapping(t *testing.T) {
	t.Setenv("PGROUTER_BACKEND_SHARD_0", "10.0.0.1:5432")
	if got := backendFor("shard-0"); got != "10.0.0.1:5432" {
		t.Fatalf("backendFor(shard-0) = %q, want 10.0.0.1:5432", got)
	}
	if got := backendFor("shard-9"); got != "127.0.0.1:5432" {
		t.Fatalf("backendFor(shard-9) default = %q, want 127.0.0.1:5432", got)
	}
}
