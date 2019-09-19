package plistener

import (
	"net"
)

type pConn struct {
	net.Conn
	listener *PListener
}

func (pConn *pConn) Close() error {
	myListener := pConn.listener
	if myListener != nil {
		myListener.connCond.L.Lock()
		myListener.currentConn--
		myListener.connCond.L.Unlock()
		tcpAddr := pConn.Conn.RemoteAddr().(*net.TCPAddr)
		var ip [16]byte
		copy(ip[:], tcpAddr.IP.To16())
		record := myListener.getRecord(ip)
		record.mut.Lock()
		defer record.mut.Unlock()
		if len(record.activeConns) > 0 {
			var index int
			for i, c := range record.activeConns {
				if c == pConn {
					index = i
					break
				}
			}
			record.activeConns[index] = record.activeConns[len(record.activeConns)-1]
			record.activeConns = record.activeConns[:len(record.activeConns)-1]
		}
		myListener.connCond.Signal()
	}
	return pConn.Conn.Close()
}
