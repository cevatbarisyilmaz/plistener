package plistener

import (
	"errors"
	"golang.org/x/net/nettest"
	"net"
	"testing"
)

func TestPConn(t *testing.T) {
	nettest.TestConn(t, func() (c1, c2 net.Conn, stop func(), err error) {
		var listener *PListener
		listener, err = getPListener()
		if err != nil {
			return
		}
		connChannel := make(chan net.Conn)
		errChannel := make(chan error)
		go func(connChannel chan net.Conn, errChannel chan error) {
			conn, err := listener.Accept()
			if err != nil {
				errChannel <- err
			}
			connChannel <- conn
		}(connChannel, errChannel)
		go func(connChannel chan net.Conn, errChannel chan error) {
			raddr, ok := listener.Addr().(*net.TCPAddr)
			if !ok {
				errChannel <- errors.New("*net.TCPAddr cast failed")
			}
			conn, err := net.DialTCP("tcp", nil, raddr)
			if err != nil {
				errChannel <- err
			}
			connChannel <- conn
		}(connChannel, errChannel)
		for {
			select {
			case newConn := <-connChannel:
				if c1 == nil {
					c1 = newConn
				} else {
					c2 = newConn
					stop = func() {
						c1.Close()
						c2.Close()
						listener.Close()
					}
					return
				}
			case err = <-errChannel:
				return
			}
		}
	})
}

func TestCastToTCPConn(t *testing.T) {
	listener, err := getPListener()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		err = listener.Close()
		if err != nil {
			t.Fatal(err)
		}
	}()
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatal("*net.TCPAddr cast failed")
	}
	go func() {
		net.DialTCP("tcp", nil, addr)
	}()
	conn, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	tcpConn, err := CastToTCPConn(conn)
	if err != nil {
		t.Fatal(err)
	}
	if tcpConn == nil {
		t.Fatal("casted *net.TCP is nil")
	}
}
