package nettimer

import (
	"net"
	"sync"
	"time"
)

//IOStat gives statistics for a single IO operation
type IOStat struct {
	Start time.Time
	Done  time.Time
	Size  int
}

//Duration returns the time spend on the IO operation
func (s *IOStat) Duration() time.Duration {
	return s.Done.Sub(s.Start)
}

//Stat gives statistics for a single connection
type Stat struct {
	StartDial   time.Time
	ConnCreated time.Time
	Reads       []IOStat
	Writes      []IOStat
	Close       time.Time
	mu          *sync.Mutex
}

//ReadDuration gives the total time spent reading from the connection
func (s *Stat) ReadDuration() time.Duration {
	var d time.Duration
	for _, i := range s.Reads {
		d += i.Duration()
	}
	return d
}

//ReadSize gives the total amount of data read from the connection
func (s *Stat) ReadSize() int64 {
	var size int64
	for _, i := range s.Reads {
		size += int64(i.Size)
	}
	return size
}

//WriteDuration gives the total time spent writing to the connection
func (s *Stat) WriteDuration() time.Duration {
	var d time.Duration
	for _, i := range s.Writes {
		d += i.Duration()
	}
	return d
}

//WriteSize gives the total amount of data written to the connection
func (s *Stat) WriteSize() int64 {
	var size int64
	for _, i := range s.Writes {
		size += int64(i.Size)
	}
	return size
}

//conn wraps net.Conn to allow us to pass a context around
type conn struct {
	net.Conn
	read  func(b []byte) (int, error)
	write func(b []byte) (int, error)
	close func() error
}

func (c *conn) Read(b []byte) (int, error) {
	if c.read != nil {
		return c.read(b)
	}
	return c.Conn.Read(b)
}

func (c *conn) Write(b []byte) (int, error) {
	if c.write != nil {
		return c.write(b)
	}
	return c.Conn.Write(b)
}

func (c *conn) Close() error {
	if c.close != nil {
		return c.close()
	}
	return c.Conn.Close()
}

//Dialer wraps a net.Dialer and returns Stats for each connection on Stats
type Dialer struct {
	*net.Dialer
	Stats <-chan *Stat
	stats chan *Stat
}

//Dial wraps net.Dialer's Dial. See that function for more infomation
func (d *Dialer) Dial(network, addr string) (net.Conn, error) {
	s := &Stat{StartDial: time.Now().UTC(), mu: new(sync.Mutex)}
	c, err := d.Dialer.Dial(network, addr)
	s.mu.Lock()
	s.ConnCreated = time.Now().UTC()
	s.mu.Unlock()

	timedConn := &conn{
		Conn: c,
		read: func(b []byte) (int, error) {
			i := IOStat{Start: time.Now().UTC()}
			n, e := c.Read(b)
			if n != 0 {
				i.Done = time.Now().UTC()
				i.Size = n
				s.mu.Lock()
				s.Reads = append(s.Reads, i)
				s.mu.Unlock()
			}
			return n, e
		},
		write: func(b []byte) (int, error) {
			i := IOStat{Start: time.Now().UTC()}
			n, e := c.Write(b)
			if n != 0 {
				i.Done = time.Now().UTC()
				i.Size = n
				s.mu.Lock()
				s.Writes = append(s.Writes, i)
				s.mu.Unlock()
			}
			return n, e
		},
		close: func() error {
			e := c.Close()
			s.mu.Lock()
			s.Close = time.Now().UTC()
			s.mu.Unlock()
			//send in goroutine so close won't block
			go func() {
				s.mu.Lock()
				d.stats <- s
				s.mu.Unlock()
			}()
			return e
		},
	}
	return timedConn, err
}

//NewDialer returns a new Dialer
func NewDialer(d *net.Dialer) *Dialer {
	c := make(chan *Stat)
	return &Dialer{Dialer: d, Stats: c, stats: c}
}
