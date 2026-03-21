package session

import (
	"fmt"
	"sync"
	"testing"

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
