package plistener

import (
	"errors"
	"net"
	"time"
)

type pConn struct {
	listener *PListener
	tcpConn  *net.TCPConn
}

func (pConn *pConn) Read(b []byte) (int, error) {
	return pConn.tcpConn.Read(b)
}

func (pConn *pConn) Write(b []byte) (int, error) {
	return pConn.tcpConn.Write(b)
}

func (pConn *pConn) Close() error {
	pConn.listener.connCond.L.Lock()
	pConn.listener.currentConn--
	pConn.listener.connCond.L.Unlock()
	tcpAddr, err := net.ResolveTCPAddr("tcp", pConn.tcpConn.RemoteAddr().String())
	if err == nil {
		var ip [16]byte
		copy(ip[:], tcpAddr.IP.To16())
		record := pConn.listener.getRecord(ip)
		record.mut.Lock()
		record.activeConns--
		record.mut.Unlock()
	}
	(*pConn.listener.connCond).Signal()
	return pConn.tcpConn.Close()
}

func (pConn *pConn) LocalAddr() net.Addr {
	return pConn.tcpConn.LocalAddr()
}

func (pConn *pConn) RemoteAddr() net.Addr {
	return pConn.tcpConn.RemoteAddr()
}

func (pConn *pConn) SetDeadline(t time.Time) error {
	return pConn.tcpConn.SetDeadline(t)
}

func (pConn *pConn) SetReadDeadline(t time.Time) error {
	return pConn.tcpConn.SetReadDeadline(t)
}

func (pConn *pConn) SetWriteDeadline(t time.Time) error {
	return pConn.tcpConn.SetWriteDeadline(t)
}

//Casts a conn created by plistener to *net.TCPConn
func CastToTCPConn(conn net.Conn) (*net.TCPConn, error) {
	a, ok := conn.(*pConn)
	if !ok {
		return nil, errors.New("casting failed")
	}
	return a.tcpConn, nil
}
