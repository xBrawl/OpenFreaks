package mainserver

import (
	"fmt"
	"strings"

	"github.com/freaks/openfreaks/internal/store"
)

type queuedMessage struct {
	conn   *Conn
	packet []byte
}

// Responses wait for the durable write; failed commits roll back memory and are never acknowledged.
func (s *Server) runOperation(operation func() bool) bool {
	defer s.store.LockState()()
	if s.storageFailed.Load() {
		return false
	}
	s.deliveryMu.Lock()
	s.staging = true
	s.pendingDelivery = nil
	s.deliveryMu.Unlock()

	s.store.Begin()
	s.store.DeferSaves(true)
	ok := operation()
	s.store.DeferSaves(false)

	var err error
	if ok {

		if dirty := s.store.DirtySet(); !dirty.Empty() {
			err = s.store.Commit(s.store.LiveDump(dirty))
		}
	}
	if !ok || err != nil {
		s.store.Rollback()
	} else {
		s.store.End()
	}

	s.deliveryMu.Lock()
	messages := s.pendingDelivery
	s.pendingDelivery = nil
	s.staging = false
	s.deliveryMu.Unlock()
	if err != nil {
		s.log.Error("durable transaction failed; main is refusing further writes", "err", err)
		s.storageFailed.Store(true)
		s.mu.RLock()
		for _, c := range s.conns {
			if c.Conn != nil {
				_ = c.Conn.Close()
			}
		}
		s.mu.RUnlock()
		s.Close()
		return false
	}
	if !ok {
		return false
	}
	for _, message := range messages {
		message.conn.enqueue(message.packet)
	}
	return true
}

func (s *Server) runReadOnly(operation func() bool) bool {
	defer s.store.LockState()()
	if s.storageFailed.Load() {
		return false
	}
	s.store.Begin()
	ok := operation()
	dirty := s.store.DirtySet()
	if dirty.Empty() {
		s.store.End()
		return ok
	}
	s.log.Error("handler marked read-only changed state; writing it anyway", "dirty", describeDirty(dirty))
	if !ok {
		s.store.Rollback()
		return false
	}
	if err := s.store.Commit(s.store.LiveDump(dirty)); err != nil {
		s.store.Rollback()
		s.log.Error("durable transaction failed; main is refusing further writes", "err", err)
		s.storageFailed.Store(true)
		s.Close()
		return false
	}
	s.store.End()
	return ok
}

func describeDirty(d *store.Dirty) string {
	parts := make([]string, 0, 4)
	add := func(name string, n int) {
		if n > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", name, n))
		}
	}
	add("accounts", len(d.Accounts))
	add("creds", len(d.Creds))
	add("alliances", len(d.Alliances))
	add("friends", len(d.Friends))
	add("battles", len(d.Battles))
	add("bonds", len(d.Bonds))
	add("receipts", len(d.Receipts))
	add("reactors", len(d.Reactors))
	if d.Core {
		parts = append(parts, "core")
	}
	return strings.Join(parts, ",")
}

func (s *Server) Healthy() bool {
	if s.storageFailed.Load() {
		return false
	}
	select {
	case <-s.shutdown:
		return false
	default:
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.listener != nil
}
