/*
Package plistener implements a wrapper around TCP Listeners against spams.

Simple usage:

	//Create a PListener from a TCP Listener
	listener := plistener.New(tcpListener)
	for {
		//Accept next connection
		conn, err := listener.Accept()
		if err != nil{
			//Handle error
		}
		go handleConn(conn) //Handle conn
	}

HTTP Server:

	//Create a PListener from a TCP Listener
	listener := plistener.New(tcpListener)
	//Init the server
	server := &http.Server{}
	//Serve HTTP over PListener
	server.Serve(listener)
*/
package plistener

import (
	"net"
	"sync"
	"time"
)

//Number of maximum connections listeners willing to keep active
var DefaultMaxConn = 1048

//Number of maximum connections listeners willing to keep active with a single remote IP
var DefaultMaxConnSingleIP = 32

//Limits for maximum number of new request over a time from the same IP to not considered as spam
var DefaultLimits = []struct {
	time.Duration
	int
}{
	{time.Second, 32},
	{time.Minute, 256},
	{time.Hour, 2048},
}

type ipRecord struct {
	activeConns    int
	history        []time.Time
	recentlyActive bool
	blocked        bool
	mut            *sync.Mutex
}

func newIPRecord() *ipRecord {
	return &ipRecord{
		activeConns:    0,
		history:        []time.Time{},
		recentlyActive: false,
		blocked:        false,
		mut:            &sync.Mutex{},
	}
}

//PListener implements the net.Listener interface with protection against spams
type PListener struct {
	TCPListener *net.TCPListener //Base listener

	MaxConn         int //Overrides DefaultMaxConn for the listener
	MaxConnSingleIP int //Overrides DefaultMaxConnSingleIP for the listener

	currentConn int
	connCond    *sync.Cond

	limits []struct {
		time.Duration
		int
	}
	maxLimitDur time.Duration

	ipRecords   map[[16]byte]*ipRecord
	ipRecordMut *sync.Mutex
}

//Returns a new PListener wraps the given TCPListener
func New(tcpListener *net.TCPListener) (pListener *PListener) {
	pListener = &PListener{
		TCPListener:     tcpListener,
		MaxConn:         DefaultMaxConn,
		currentConn:     0,
		connCond:        sync.NewCond(&sync.Mutex{}),
		MaxConnSingleIP: DefaultMaxConnSingleIP,
		ipRecords:       map[[16]byte]*ipRecord{},
		ipRecordMut:     &sync.Mutex{},
		limits:          DefaultLimits,
		maxLimitDur:     DefaultLimits[len(DefaultLimits)-1].Duration,
	}
	go pListener.cleanup()
	return
}

//Accept implements the Accept method in the net.Listener interface; it waits for the next non-spam call and returns a generic Conn.
func (pListener *PListener) Accept() (conn net.Conn, err error) {
	blocked := false
	var tcpConn *net.TCPConn
	var raddr *net.TCPAddr
	var record *ipRecord
	defer func() {
		if err != nil {
			tcpConn = nil
			pListener.connCond.L.Lock()
			pListener.currentConn--
			pListener.connCond.L.Unlock()
		}
	}()
	for {
		if blocked {
			blocked = false
			record.mut.Unlock()
			tcpConn = nil
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
		blocked = record.blocked
		if blocked {
			continue
		}
		if record.activeConns >= pListener.MaxConnSingleIP {
			blocked = true
		}
		if blocked {
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
			for _, limit := range pListener.limits {
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
		record.activeConns++
		record.mut.Unlock()
		conn = &pConn{tcpConn: tcpConn, listener: pListener}
		return
	}
}

//Close stops listening on the TCP address and erases the internal records. Already Accepted connections are not closed.
func (pListener *PListener) Close() error {
	pListener.ipRecordMut.Lock()
	pListener.ipRecords = nil
	pListener.ipRecordMut.Unlock()
	return pListener.TCPListener.Close()
}

//Addr returns the listener's network address, a *TCPAddr. The Addr returned is shared by all invocations of Addr, so do not modify it.
func (pListener PListener) Addr() net.Addr {
	return pListener.TCPListener.Addr()
}

//SetLimits overrides the default limits for listener.
//Limits can be sorted in any order.
//A duration and int duple specifies maximum allowed new connections from an IP in a time interval.
func (pListener *PListener) SetLimits(limits []struct {
	time.Duration
	int
}) {
	pListener.limits = limits
	max := time.Nanosecond
	for _, limit := range limits {
		if limit.Duration > max {
			max = limit.Duration
		}
	}
	pListener.maxLimitDur = max
}

//Blocks any future connections with given IP
func (pListener *PListener) Ban(ip net.IP) {
	var ipByte [16]byte
	copy(ipByte[:], ip)
	record := pListener.getRecord(ipByte)
	record.mut.Lock()
	record.blocked = true
	record.history = []time.Time{}
	record.mut.Unlock()
}

//Revokes the ban for given IP
func (pListener *PListener) RevokeBan(ip net.IP) {
	var ipByte [16]byte
	copy(ipByte[:], ip)
	record := pListener.getRecord(ipByte)
	record.mut.Lock()
	record.blocked = false
	record.mut.Unlock()
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
		time.Sleep(pListener.maxLimitDur * 2)
		pListener.ipRecordMut.Lock()
		if pListener.ipRecords == nil {
			pListener.ipRecordMut.Unlock()
			break
		}
		for ip, record := range pListener.ipRecords {
			record.mut.Lock()
			if !record.recentlyActive {
				if record.activeConns == 0 && !record.blocked {
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
