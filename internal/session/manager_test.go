package session

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManager_AddAndGet(t *testing.T) {
	m := NewManager()
	s := newMock("sess1")
	m.Add(s)
	got, ok := m.Get("sess1")
	require.True(t, ok)
	assert.Equal(t, "sess1", got.ID())
}

func TestManager_GetNonexistent(t *testing.T) {
	m := NewManager()
	_, ok := m.Get("missing")
	assert.False(t, ok)
}

func TestManager_List(t *testing.T) {
	m := NewManager()
	m.Add(newMock("a"))
	m.Add(newMock("b"))
	list := m.List()
	assert.Len(t, list, 2)
}

func TestManager_Remove_ClosesSession(t *testing.T) {
	m := NewManager()
	s := newMock("sess1")
	m.Add(s)
	err := m.Remove("sess1")
	require.NoError(t, err)
	assert.True(t, s.closed)
	_, ok := m.Get("sess1")
	assert.False(t, ok)
}

func TestManager_Remove_Nonexistent(t *testing.T) {
	m := NewManager()
	err := m.Remove("missing")
	assert.Error(t, err)
}

func TestManager_CloseAll(t *testing.T) {
	m := NewManager()
	s1 := newMock("a")
	s2 := newMock("b")
	m.Add(s1)
	m.Add(s2)
	m.CloseAll()
	assert.True(t, s1.closed)
	assert.True(t, s2.closed)
	assert.Empty(t, m.List())
}

func TestManager_ConcurrentAccess(t *testing.T) {
	m := NewManager()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("sess%d", i)
			s := newMock(id)
			m.Add(s)
			m.Get(id)
			m.List()
		}(i)
	}
	wg.Wait()
}

func TestManager_Subscribe_ReplaysBufferedOutputAndStreamsNewOutput(t *testing.T) {
	m := NewManager()
	s := newMock("sess1")
	m.Add(s)

	_, err := s.Write([]byte("before"))
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		snapshot, updates, unsubscribe, ok := m.Subscribe("sess1")
		if !ok {
			return false
		}
		defer unsubscribe()

		if string(snapshot) != "before" {
			return false
		}

		_, err = s.Write([]byte("after"))
		require.NoError(t, err)

		select {
		case got := <-updates:
			return string(got) == "after"
		case <-time.After(100 * time.Millisecond):
			return false
		}
	}, time.Second, 10*time.Millisecond)
}

func TestManager_Subscribe_ClosedSessionReturnsSnapshotAndClosedStream(t *testing.T) {
	m := NewManager()
	s := newMock("sess1")
	m.Add(s)

	_, err := s.Write([]byte("before close"))
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		snapshot, _, unsubscribe, ok := m.Subscribe("sess1")
		if !ok {
			return false
		}
		defer unsubscribe()
		return string(snapshot) == "before close"
	}, time.Second, 10*time.Millisecond)

	require.NoError(t, s.Close())

	require.Eventually(t, func() bool {
		snapshot, updates, unsubscribe, ok := m.Subscribe("sess1")
		if !ok {
			return false
		}
		defer unsubscribe()

		if string(snapshot) != "before close" {
			return false
		}

		select {
		case _, stillOpen := <-updates:
			return !stillOpen
		case <-time.After(100 * time.Millisecond):
			return false
		}
	}, time.Second, 10*time.Millisecond)
}

func TestManagedSession_SubscribeStillWorksWhilePublishWaitsOnSlowSubscriber(t *testing.T) {
	slowSubscriber := make(chan []byte, 1)
	slowSubscriber <- []byte("already full")

	entry := &managedSession{
		subscribers: map[int]chan []byte{
			0: slowSubscriber,
		},
	}

	go entry.publish([]byte("next chunk"))

	done := make(chan struct{})
	go func() {
		_, updates, unsubscribe := entry.subscribe()
		unsubscribe()
		select {
		case _, ok := <-updates:
			require.False(t, ok)
		default:
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("subscribe blocked behind a slow subscriber")
	}
}
