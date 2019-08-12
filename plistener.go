/*
Package plistener is a wrapper around TCP Listeners to filter spam requests.

It has capability to detect and filter out spam requests as well as temporary and permanent bans on  IPs.

*/
package plistener

import (
	"net"
	"sync"
	"time"
)

// DefaultMaxConn is maximum number of connections listeners willing to keep active.
// Changing DefaultMaxConn only affects future listeners.
// To change a currently active listener use MaxConn field of PListener struct.
var DefaultMaxConn = 1048

// DefaultMaxConnSingleIP is maximum number of connections listeners willing to keep active with a single IP.
// Changing DefaultMaxConnSingleIP only affects future listeners.
// To change a currently active listener use MaxConnSingleIP field of PListener struct.
var DefaultMaxConnSingleIP = 16

// Limiter is a slice of pairs of durations and amounts of maximum permitted new connections for IP addresses during the stated duration.
type Limiter []struct {
	Duration time.Duration
	Int      int
}

// DefaultLimiter is the default Limiter of listeners.
// Changing DefaultLimiter only affects future listeners.
// To change a currently active listener use SetLimiter function of PListener struct.
var DefaultLimiter = Limiter{
	{time.Second, 32},
	{time.Minute, 256},
	{time.Hour, 2048},
}

type ipRecord struct {
	activeConns    []*pConn
	history        []time.Time
	recentlyActive bool
	blocked        bool
	blockedUntil   *time.Time
	privileged     bool
	mut            *sync.Mutex
}

func newIPRecord() *ipRecord {
	return &ipRecord{
		activeConns:    []*pConn{},
		history:        []time.Time{},
		recentlyActive: false,
		blocked:        false,
		blockedUntil:   nil,
		privileged:     false,
		mut:            &sync.Mutex{},
	}
}

// PListener implements the net.Listener interface with protection against spams.
type PListener struct {
	*net.TCPListener

	// MaxConn is maximum number of connections listener willing to keep active.
	// Default value is DefaultMaxConn.
	MaxConn int

	// MaxConnSingleIP is maximum number of connections listener willing to keep active with single IP.
	// Default value is DefaultMaxConnSingleIP.
	MaxConnSingleIP int

	// OnSpam is called if any unbanned IP exceeds the given Limiter or MaxConnSingleIP.
	// It can be used to notify the application about the spams or impose a ban on the IP.
	//
	// No matter the behaviour of OnSpam function, the spam requests that exceeds the limiter or maximum connection quota gets filtered out.
	//
	// Default value is nil.
	OnSpam func(net.IP)

	currentConn int
	connCond    *sync.Cond

	limiter     Limiter
	maxLimitDur time.Duration

	ipRecords   map[[16]byte]*ipRecord
	ipRecordMut *sync.Mutex
}

// New returns a new PListener that wraps the given TCPListener with anti-spam capabilities.
func New(tcpListener *net.TCPListener) (pListener *PListener) {
	pListener = &PListener{
		TCPListener:     tcpListener,
		MaxConn:         DefaultMaxConn,
		MaxConnSingleIP: DefaultMaxConnSingleIP,
		OnSpam:          nil,
		currentConn:     0,
		connCond:        sync.NewCond(&sync.Mutex{}),
		limiter:         DefaultLimiter,
		maxLimitDur:     DefaultLimiter[len(DefaultLimiter)-1].Duration,
		ipRecords:       map[[16]byte]*ipRecord{},
		ipRecordMut:     &sync.Mutex{},
	}
	go pListener.cleanup()
	return
}

// Accept implements the Accept method in the net.Listener interface; it waits for the next non-spam call and returns a net.Conn.
func (pListener *PListener) Accept() (conn net.Conn, err error) {
	blocked := false
	banned := false
	var tcpConn *net.TCPConn
	var raddr *net.TCPAddr
	var record *ipRecord
	defer func() {
		if err != nil {
			if tcpConn != nil {
				_ = tcpConn.Close()
			}
			pListener.connCond.L.Lock()
			pListener.currentConn--
			pListener.connCond.L.Unlock()
		} else {
			record.mut.Unlock()
		}
	}()
	for {
		if blocked {
			err = tcpConn.Close()
			if err != nil {
				return
			}
			record.mut.Unlock()
			if !banned && pListener.OnSpam != nil {
				pListener.OnSpam(raddr.IP)
			}
			banned = false
			blocked = false
			pListener.connCond.L.Lock()
			pListener.currentConn--
		} else {
			pListener.connCond.L.Lock()
		}
		for pListener.currentConn >= pListener.MaxConn {
			pListener.connCond.Wait()
		}
		pListener.currentConn++
		pListener.connCond.L.Unlock()
		tcpConn, err = pListener.TCPListener.AcceptTCP()
		if err != nil {
			return
		}
		now := time.Now()
		raddr = tcpConn.RemoteAddr().(*net.TCPAddr)
		var ip [16]byte
		copy(ip[:], raddr.IP.To16())
		record = pListener.getRecord(ip)
		record.mut.Lock()
		if !record.privileged {
			if record.blocked {
				blocked = true
				banned = true
				continue
			}
			if record.blockedUntil != nil {
				if record.blockedUntil.After(now) {
					blocked = true
					banned = true
					continue
				}
				record.blockedUntil = nil
			}
			if len(record.activeConns) >= pListener.MaxConnSingleIP {
				blocked = true
				continue
			}
			count := 0
			cut := -1
			for index, historyRecord := range record.history {
				interval := now.Sub(historyRecord)
				if interval > pListener.maxLimitDur {
					cut = index
					continue
				}
				count++
				for _, limit := range pListener.limiter {
					if limit.Duration > interval {
						if count >= limit.Int {
							blocked = true
							break
						}
					}
				}
				if blocked {
					break
				}
			}
			record.recentlyActive = true
			record.history = record.history[cut+1:]
			if blocked {
				continue
			}
			record.history = append(record.history, now)
		}
		pconn := &pConn{TCPConn: tcpConn, listener: pListener}
		record.activeConns = append(record.activeConns, pconn)
		conn = pconn
		return
	}
}

// Close closes the underlying TCP listener and erases the pointers from created connections.
// To remove internal records from the memory, remove all the pointers pointing to PListener.
func (pListener *PListener) Close() error {
	pListener.ipRecordMut.Lock()
	defer pListener.ipRecordMut.Unlock()
	for _, record := range pListener.ipRecords {
		record.mut.Lock()
		if record.activeConns != nil {
			for _, conn := range record.activeConns {
				conn.listener = nil
			}
		}
		record.mut.Unlock()
	}
	return pListener.TCPListener.Close()
}

// SetLimiter overrides the default limiter for listener.
// Limits can be sorted in any order.
// A duration and int duple specifies maximum allowed new connections from an IP in a time interval.
func (pListener *PListener) SetLimiter(limiter Limiter) {
	pListener.limiter = limiter
	max := time.Nanosecond
	for _, limit := range limiter {
		if limit.Duration > max {
			max = limit.Duration
		}
	}
	pListener.maxLimitDur = max
}

// Ban permanently blocks any future connections and closes all the current connections with the given IP created by this listener.
func (pListener *PListener) Ban(ip net.IP) {
	var ipByte [16]byte
	copy(ipByte[:], ip.To16())
	record := pListener.getRecord(ipByte)
	record.mut.Lock()
	defer record.mut.Unlock()
	record.blocked = true
	record.blockedUntil = nil
	record.privileged = false
	if record.activeConns != nil {
		for _, c := range record.activeConns {
			_ = c.TCPConn.Close()
		}
	}
	record.activeConns = nil
	record.history = nil
}

// TempBan temporarily blocks future connections with the given IP until the given time.
// It also closes all the current connections with the given IP created by this listener.
func (pListener *PListener) TempBan(ip net.IP, until time.Time) {
	var ipByte [16]byte
	copy(ipByte[:], ip.To16())
	record := pListener.getRecord(ipByte)
	record.mut.Lock()
	defer record.mut.Unlock()
	record.blocked = false
	record.blockedUntil = &until
	record.privileged = false
	if record.activeConns != nil {
		for _, c := range record.activeConns {
			_ = c.TCPConn.Close()
		}
	}
	record.activeConns = []*pConn{}
	record.history = []time.Time{}
}

// RevokeBan revokes the ban on the given IP.
func (pListener *PListener) RevokeBan(ip net.IP) {
	var ipByte [16]byte
	copy(ipByte[:], ip.To16())
	record := pListener.getRecord(ipByte)
	record.mut.Lock()
	defer record.mut.Unlock()
	record.blocked = false
	record.blockedUntil = nil
	record.activeConns = []*pConn{}
	record.history = []time.Time{}
}

// GivePrivilege removes the limitations of the limiter and MaxConnSingleIP for the given IP.
// However if MaxConn quota is reached, requests from privileged IPs are still get blocked.
func (pListener *PListener) GivePrivilege(ip net.IP) {
	var ipByte [16]byte
	copy(ipByte[:], ip.To16())
	record := pListener.getRecord(ipByte)
	record.mut.Lock()
	defer record.mut.Unlock()
	record.blocked = false
	record.blockedUntil = nil
	if record.activeConns == nil {
		record.activeConns = []*pConn{}
	}
	record.history = nil
	record.privileged = true
}

// RevokePrivilege removes the privilege of the given IP.
func (pListener *PListener) RevokePrivilege(ip net.IP) {
	var ipByte [16]byte
	copy(ipByte[:], ip.To16())
	record := pListener.getRecord(ipByte)
	record.mut.Lock()
	defer record.mut.Unlock()
	record.privileged = false
	record.history = []time.Time{}
}

func (pListener *PListener) getRecord(ip [16]byte) (record *ipRecord) {
	pListener.ipRecordMut.Lock()
	defer pListener.ipRecordMut.Unlock()
	if pListener.ipRecords == nil {
		record = newIPRecord()
		return
	}
	record = pListener.ipRecords[ip]
	if record != nil {
		return
	}
	record = newIPRecord()
	pListener.ipRecords[ip] = record
	return
}

func (pListener *PListener) cleanup() {
	for {
		if pListener.maxLimitDur*2 > time.Minute {
			time.Sleep(pListener.maxLimitDur * 2)
		} else {
			time.Sleep(time.Minute)
		}
		pListener.ipRecordMut.Lock()
		if pListener.ipRecords == nil {
			pListener.ipRecordMut.Unlock()
			break
		}
		now := time.Now()
		for ip, record := range pListener.ipRecords {
			record.mut.Lock()
			if record.blocked || record.privileged {
				record.mut.Unlock()
				continue
			}
			if record.blockedUntil != nil {
				if now.After(*record.blockedUntil) {
					record.blockedUntil = nil
				} else {
					record.mut.Unlock()
					continue
				}
			}
			if !record.recentlyActive {
				if len(record.activeConns) == 0 {
					delete(pListener.ipRecords, ip)
					record.mut.Unlock()
					continue
				}
				record.history = []time.Time{}
			}
			record.recentlyActive = false
			record.mut.Unlock()
		}
		pListener.ipRecordMut.Unlock()
	}
}
