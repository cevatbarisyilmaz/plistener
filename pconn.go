package plistener

import (
	"net"
	"time"
)

//PConn implements the net.Conn interface.
// It clones the behaviour of a TCPConn except it notifies the origin listener when it is closed.
type PConn struct {
	listener *PListener
	TCPConn  *net.TCPConn
}

func (pConn *PConn) Read(b []byte) (int, error) {
	return pConn.TCPConn.Read(b)
}

func (pConn *PConn) Write(b []byte) (int, error) {
	return pConn.TCPConn.Write(b)
}

func (pConn *PConn) Close() error {
	pConn.listener.connCond.L.Lock()
	pConn.listener.currentConn--
	pConn.listener.connCond.L.Unlock()
	return pConn.TCPConn.Close()
}

func (pConn *PConn) LocalAddr() net.Addr {
	return pConn.TCPConn.LocalAddr()
}

func (pConn *PConn) RemoteAddr() net.Addr {
	return pConn.TCPConn.RemoteAddr()
}

func (pConn *PConn) SetDeadline(t time.Time) error {
	return pConn.TCPConn.SetDeadline(t)
}

func (pConn *PConn) SetReadDeadline(t time.Time) error {
	return pConn.TCPConn.SetReadDeadline(t)
}

func (pConn *PConn) SetWriteDeadline(t time.Time) error {
	return pConn.TCPConn.SetWriteDeadline(t)
}
