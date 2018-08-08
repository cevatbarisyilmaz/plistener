/*
Package plistener implements a wrapper around TCP Listeners against spams.

Simple usage:

	//Create a PListener from a TCP Listener
	listener := plistener.New(tcpListener)
	for{
		//Start accepting connections
		TCPConn, err := listener.Accept()
		if err != nil{
			//Handle error
		}
		go handleConn(TCPConn)
	}

HTTP Server:

	//Create a PListener from a TCP Listener
	listener := plistener.New(tcpListener)
	//Init the server
	server := &http.Server{}
	//Serve over PListener
	server.Serve(listener)
*/
package plistener

import (
	"net"
	"sync"
	"time"
)

//Number of maximum connections listener willing to keep active
var DefaultMaxConn = 1048

//Limits for maximum number of new request over a time from the same IP to not considered as spam
var DefaultLimits = []struct {
	time.Duration
	int
}{
	{time.Second, 32},
	{time.Minute, 256},
	{time.Hour, 2048},
}

//PListener implements the net.Listener interface with protection against spams
type PListener struct {
	TcpListener *net.TCPListener //Base listener

	maxConn     int
	currentConn int
	connCond    *sync.Cond

	blackList map[[16]byte]bool
	blackMut  *sync.Mutex

	limits []struct {
		time.Duration
		int
	}
	maxLimitDur time.Duration

	records   map[[16]byte][]time.Time
	recordMut *sync.Mutex

	recentIPs map[[16]byte]bool
	recentMut *sync.Mutex

	closed bool
}

//Returns a new PListener wraps the given TCPListener
func New(tcpListener *net.TCPListener) (pListener *PListener) {
	pListener = &PListener{
		TcpListener: tcpListener,
		maxConn:     DefaultMaxConn,
		currentConn: 0,
		connCond:    sync.NewCond(&sync.Mutex{}),
		blackList:   map[[16]byte]bool{},
		blackMut:    &sync.Mutex{},
		limits:      DefaultLimits,
		maxLimitDur: DefaultLimits[len(DefaultLimits)-1].Duration,
		records:     map[[16]byte][]time.Time{},
		recordMut:   &sync.Mutex{},
		recentIPs:   map[[16]byte]bool{},
		recentMut:   &sync.Mutex{},
		closed:      false,
	}
	go pListener.clearRecords()
	return
}

func (pListener *PListener) Accept() (net.Conn, error) {
	return pListener.AcceptTCP()
}

func (pListener *PListener) Close() error {
	pListener.closed = true
	return pListener.TcpListener.Close()
}

func (pListener PListener) Addr() net.Addr {
	return pListener.TcpListener.Addr()
}

func (pListener *PListener) AcceptTCP() (*PConn, error) {
	for {
		pListener.connCond.L.Lock()
		for pListener.currentConn >= pListener.maxConn {
			pListener.connCond.Wait()
		}
		pListener.currentConn++
		pListener.connCond.L.Unlock()
		conn, err := pListener.TcpListener.AcceptTCP()
		if err != nil {
			pListener.connCond.L.Lock()
			pListener.currentConn--
			pListener.connCond.L.Unlock()
			return nil, err
		}
		raddr, err := net.ResolveTCPAddr("tcp", conn.RemoteAddr().String())
		if err != nil {
			conn.Close()
			pListener.connCond.L.Lock()
			pListener.currentConn--
			pListener.connCond.L.Unlock()
			return nil, err
		}
		var ip [16]byte
		copy(ip[:], raddr.IP.To16())
		pListener.blackMut.Lock()
		blocked := pListener.blackList[ip]
		pListener.blackMut.Unlock()
		if blocked {
			conn.Close()
			continue
		}
		pListener.recentMut.Lock()
		pListener.recentIPs[ip] = true
		pListener.recentMut.Unlock()
		pListener.recordMut.Lock()
		records := pListener.records[ip]
		pListener.recordMut.Unlock()
		if records != nil {
			limited := false
			now := time.Now()
			count := 0
			cut := -1
			for index, record := range records {
				interval := now.Sub(record)
				if interval > pListener.maxLimitDur {
					cut = index
					continue
				}
				count++
				for _, limit := range pListener.limits {
					if limit.Duration > interval {
						if count > limit.int {
							limited = true
							break
						}
					}
				}
				if limited {
					break
				}
			}

			pListener.recordMut.Lock()
			pListener.records[ip] = records[cut+1:]
			pListener.recordMut.Unlock()
			if limited {
				conn.Close()
				continue
			}
			pListener.recordMut.Lock()
			pListener.records[ip] = append(pListener.records[ip], now)
			pListener.recordMut.Unlock()
		} else {
			pListener.recordMut.Lock()
			pListener.records[ip] = []time.Time{time.Now()}
			pListener.recordMut.Unlock()
		}
		return &PConn{TCPConn: conn, listener: pListener}, nil
	}
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

//SetMaxConn overrides the default maximum connection number for the listener.
func (pListener *PListener) SetMaxConn(maxConn int) {
	pListener.maxConn = maxConn
}

func (pListener *PListener) clearRecords() {
	for {
		time.Sleep(pListener.maxLimitDur)
		if pListener.closed {
			break
		}
		pListener.recordMut.Lock()
		records := pListener.records
		pListener.recordMut.Unlock()
		for ip := range records {
			pListener.recentMut.Lock()
			if !pListener.recentIPs[ip] {
				delete(pListener.recentIPs, ip)
				pListener.recentMut.Unlock()
				pListener.recordMut.Lock()
				delete(pListener.records, ip)
				pListener.recordMut.Unlock()
			} else {
				pListener.recentMut.Unlock()
			}
		}
	}
}
