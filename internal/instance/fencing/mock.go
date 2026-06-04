/*
Copyright 2026 keiailab.

Licensed under the MIT License. See the LICENSE file for details.
*/

package fencing

import (
	"context"
	"sync"
	"sync/atomic"
)

// Mock은 단위 테스트용 in-memory Fencer 구현이다.
type Mock struct {
	fenced atomic.Bool

	mu    sync.Mutex
	calls struct {
		MarkFenced int
		Unfence    int
		IsFenced   int
	}
}

// NewMock은 unfenced 상태로 시작하는 Mock을 만든다.
func NewMock() *Mock { return &Mock{} }

// SetFenced는 시험자가 임의 상태로 전이시킨다.
func (m *Mock) SetFenced(v bool) { m.fenced.Store(v) }

// MarkFenced는 fence flag를 켜고 호출 횟수를 기록한다.
func (m *Mock) MarkFenced(_ context.Context) error {
	m.mu.Lock()
	m.calls.MarkFenced++
	m.mu.Unlock()
	m.fenced.Store(true)
	return nil
}

// Unfence는 fence flag를 끄고 호출 횟수를 기록한다.
func (m *Mock) Unfence(_ context.Context) error {
	m.mu.Lock()
	m.calls.Unfence++
	m.mu.Unlock()
	m.fenced.Store(false)
	return nil
}

// IsFenced는 현재 flag를 반환한다.
func (m *Mock) IsFenced(_ context.Context) (bool, error) {
	m.mu.Lock()
	m.calls.IsFenced++
	m.mu.Unlock()
	return m.fenced.Load(), nil
}

// VerifyNotFenced는 fenced=true면 ErrFenced를 반환한다.
func (m *Mock) VerifyNotFenced(ctx context.Context) error {
	fenced, err := m.IsFenced(ctx)
	if err != nil {
		return err
	}
	if fenced {
		return ErrFenced
	}
	return nil
}

// Calls는 호출 카운터의 스냅샷을 반환한다.
func (m *Mock) Calls() (markFenced, unfence, isFenced int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls.MarkFenced, m.calls.Unfence, m.calls.IsFenced
}

// Compile-time guard.
var _ Fencer = (*Mock)(nil)
