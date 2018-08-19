//Package plistener is a wrapper around TCP Listeners to filter spam requests.
package plistener

import (
	"net"
	"sync"
	"time"
)

type timeoutError struct{}

func (e timeoutError) Error() string   { return "timeout error" }
func (e timeoutError) Timeout() bool   { return true }
func (e timeoutError) Temporary() bool { return false }

var errTimeout = net.Error(timeoutError{})

// DefaultMaxConn is maximum number of connections listeners willing to keep active.
// Changing DefaultMaxConn only affects future listeners.
// To change currently active listeners use MaxConn field of PListener struct.
var DefaultMaxConn = 1048

// DefaultMaxConnSingleIP is maximum number of connections listeners willing to keep active with a single IP.
// Changing DefaultMaxConnSingleIP only affects future listeners.
// To change currently active listeners use MaxConnSingleIP field of PListener struct.
var DefaultMaxConnSingleIP = 32

// Limiter is a slice of pairs of durations and amounts of maximum permitted new connections for IP addresses during the stated duration.
type Limiter []struct {
	time.Duration
	int
}

// DefaultLimiter is the default Limiter that will be used for the future listeners.
// Changing DefaultLimiter only affects future listeners.
// To change currently active listeners use SetLimiter function of PListener struct.
var DefaultLimiter = Limiter{
	{time.Second, 32},
	{time.Minute, 256},
	{time.Hour, 2048},
}

type ipRecord struct {
	activeConns    []net.Conn
	history        []time.Time
	recentlyActive bool
	blocked        bool
	blockedUntil   *time.Time
	privileged     bool
	mut            *sync.Mutex
}

func newIPRecord() *ipRecord {
	return &ipRecord{
		activeConns:    []net.Conn{},
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
	// TCPListener is the underlying listener for the PListener.
	// It can be used to bypass PListener anytime.
	// However concurrent usage of TCPListener and PListener may cause instabilities.
	TCPListener *net.TCPListener

	// MaxConn is maximum number of connections listener willing to keep active.
	// Default value is DefaultMaxConn.
	MaxConn int

	// MaxConnSingleIP is maximum number of connections listener willing to keep active with single IP.
	// Default value is DefaultMaxConnSingleIP.
	MaxConnSingleIP int

	// OnSpam is called if any unbanned IP exceeds the given limiter.
	// It can be used to notify the application about the spams or impose a ban on the IP.
	// No matter the behaviour of OnSpam function, the spam requests that exceeds the limiter gets filtered out, unless the IP is given a privilege.
	// Default value is nil
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

// Accept implements the Accept method in the net.Listener interface; it waits for the next non-spam call and returns a generic Conn.
func (pListener *PListener) Accept() (conn net.Conn, err error) {
	return pListener.accept(nil)
}

//Close stops listening on the TCP address and erases the internal records. Already Accepted connections are not closed.
func (pListener *PListener) Close() error {
	pListener.ipRecordMut.Lock()
	pListener.ipRecords = nil
	pListener.ipRecordMut.Unlock()
	return pListener.TCPListener.Close()
}

//Addr returns the listener's network address, a *TCPAddr. The Addr returned is shared by all invocations of Addr, so do not modify it.
func (pListener *PListener) Addr() net.Addr {
	return pListener.TCPListener.Addr()
}

// AcceptTimeout is like Accept, except it returns an error if no new non-spam request come during the given duration.
// Returned timeout error implements net.Error interface.
func (pListener PListener) AcceptTimeout(duration time.Duration) (conn net.Conn, err error) {
	return pListener.accept(&duration)
}

//SetLimiter overrides the default limiter for listener.
//Limits can be sorted in any order.
//A duration and int duple specifies maximum allowed new connections from an IP in a time interval.
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
	copy(ipByte[:], ip)
	record := pListener.getRecord(ipByte)
	record.mut.Lock()
	defer record.mut.Unlock()
	record.blocked = true
	record.blockedUntil = nil
	record.privileged = false
	for _, c := range record.activeConns {
		tcpConn, err := CastToTCPConn(c)
		if err != nil {
			tcpConn.Close()
		}
	}
	record.activeConns = nil
	record.history = nil
}

// Ban temporarily blocks future connections with the given IP until the given time.
// It also closes all the current connections with the given IP created by this listener.
func (pListener *PListener) TempBan(ip net.IP, until time.Time) {
	var ipByte [16]byte
	copy(ipByte[:], ip)
	record := pListener.getRecord(ipByte)
	record.mut.Lock()
	defer record.mut.Unlock()
	record.blocked = false
	record.blockedUntil = &until
	record.privileged = false
	for _, c := range record.activeConns {
		tcpConn, err := CastToTCPConn(c)
		if err != nil {
			tcpConn.Close()
		}
	}
	record.activeConns = []net.Conn{}
	record.history = []time.Time{}
}

// RevokeBan revokes the ban on the given IP.
func (pListener *PListener) RevokeBan(ip net.IP) {
	var ipByte [16]byte
	copy(ipByte[:], ip)
	record := pListener.getRecord(ipByte)
	record.mut.Lock()
	defer record.mut.Unlock()
	record.blocked = false
	record.blockedUntil = nil
	record.activeConns = []net.Conn{}
	record.history = []time.Time{}
}

// GivePrivilege removes the limitations of the limiter and MaxConnSingleIP for the given IP.
// However if MaxConn quota is reached, requests from privileged IPs are still get blocked.
func (pListener *PListener) GivePrivilege(ip net.IP) {
	var ipByte [16]byte
	copy(ipByte[:], ip)
	record := pListener.getRecord(ipByte)
	record.mut.Lock()
	defer record.mut.Unlock()
	record.blocked = false
	record.blockedUntil = nil
	if record.activeConns == nil {
		record.activeConns = []net.Conn{}
	}
	record.history = nil
	record.privileged = true
}

// RevokePrivilege removes the privilege of the given IP.
func (pListener *PListener) RevokePrivilege(ip net.IP) {
	var ipByte [16]byte
	copy(ipByte[:], ip)
	record := pListener.getRecord(ipByte)
	record.mut.Lock()
	defer record.mut.Unlock()
	record.privileged = false
	record.history = []time.Time{}
}

func (pListener *PListener) accept(timeout *time.Duration) (conn net.Conn, err error) {
	callTime := time.Now()
	blocked := false
	banned := false
	var tcpConn *net.TCPConn
	var raddr *net.TCPAddr
	var record *ipRecord
	defer func() {
		if err != nil {
			if tcpConn != nil {
				tcpConn.Close()
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
			tcpConn.Close()
			if !banned && pListener.OnSpam != nil {
				go pListener.OnSpam(raddr.IP)
			}
			record.mut.Unlock()
			banned = false
			blocked = false
			pListener.connCond.L.Lock()
			pListener.currentConn--
		} else {
			pListener.connCond.L.Lock()
		}
		if timeout == nil {
			for pListener.currentConn >= pListener.MaxConn {
				pListener.connCond.Wait()
			}
			pListener.currentConn++
			pListener.connCond.L.Unlock()
			pListener.TCPListener.SetDeadline(time.Time{})
			tcpConn, err = pListener.TCPListener.AcceptTCP()
		} else {
			cond := sync.NewCond(&sync.Mutex{})
			awaken := false
			go func() {
				for pListener.currentConn >= pListener.MaxConn {
					pListener.connCond.Wait()
				}
				pListener.currentConn++
				pListener.connCond.L.Unlock()
				cond.L.Lock()
				defer cond.L.Unlock()
				if awaken {
					return
				}
				awaken = true
				pListener.TCPListener.SetDeadline(callTime.Add(*timeout))
				tcpConn, err = pListener.TCPListener.AcceptTCP()
				cond.Signal()
			}()
			go func() {
				time.Sleep(callTime.Add(*timeout).Sub(time.Now()))
				cond.L.Lock()
				defer cond.L.Unlock()
				if awaken {
					return
				}
				awaken = true
				err = errTimeout
				cond.Signal()
			}()
			cond.L.Lock()
			cond.Wait()
		}
		now := time.Now()
		if err != nil {
			return
		}
		raddr, err = net.ResolveTCPAddr("tcp", tcpConn.RemoteAddr().String())
		if err != nil {
			return
		}
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
						if count >= limit.int {
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
		conn = &pConn{tcpConn: tcpConn, listener: pListener}
		record.activeConns = append(record.activeConns, conn)
		return
	}
}

func (pListener *PListener) getRecord(ip [16]byte) (record *ipRecord) {
	pListener.ipRecordMut.Lock()
	defer pListener.ipRecordMut.Unlock()
	if pListener.ipRecords == nil {
		return newIPRecord()
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
		for ip, record := range pListener.ipRecords {
			record.mut.Lock()
			if record.blocked || record.blockedUntil != nil || record.privileged {
				record.mut.Unlock()
				continue
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
