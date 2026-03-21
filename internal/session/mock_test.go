package session

import "io"

type mockSession struct {
	id     string
	title  string
	typ    Type
	state  State
	buf    chan []byte
	closed bool
}

func newMock(id string) *mockSession {
	return &mockSession{
		id:    id,
		title: id,
		typ:   TypeLocal,
		state: StateConnected,
		buf:   make(chan []byte, 64),
	}
}

func (m *mockSession) ID() string    { return m.id }
func (m *mockSession) Type() Type    { return m.typ }
func (m *mockSession) Title() string { return m.title }
func (m *mockSession) State() State  { return m.state }

func (m *mockSession) Read(p []byte) (int, error) {
	data, ok := <-m.buf
	if !ok {
		return 0, io.EOF
	}
	n := copy(p, data)
	return n, nil
}

func (m *mockSession) Write(p []byte) (int, error) {
	if m.closed {
		return 0, io.ErrClosedPipe
	}
	cp := make([]byte, len(p))
	copy(cp, p)
	m.buf <- cp
	return len(p), nil
}

func (m *mockSession) Resize(cols, rows uint16) error { return nil }

func (m *mockSession) Close() error {
	if !m.closed {
		m.closed = true
		close(m.buf)
	}
	return nil
}
