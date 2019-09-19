package plistener_test

import (
	"github.com/cevatbarisyilmaz/plistener"
	"golang.org/x/net/nettest"
	"net"
	"testing"
)

func TestPConn(t *testing.T) {
	nettest.TestConn(t, func() (c1, c2 net.Conn, stop func(), err error) {
		var listener *plistener.PListener
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
				return
			}
			connChannel <- conn
		}(connChannel, errChannel)
		go func(connChannel chan net.Conn, errChannel chan error) {
			raddr := listener.Addr().(*net.TCPAddr)
			conn, err := net.DialTCP("tcp", nil, raddr)
			if err != nil {
				errChannel <- err
				return
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
						_ = c1.Close()
						_ = c2.Close()
						err = listener.Close()
						if err != nil {
							t.Error(err)
						}
					}
					return
				}
			case err = <-errChannel:
				return
			}
		}
	})
}
