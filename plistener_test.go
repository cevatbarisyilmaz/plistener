package plistener_test

import (
	"github.com/cevatbarisyilmaz/plistener"
	"log"
	"net"
	"sync"
	"testing"
	"time"
)

func getPListener() (*plistener.PListener, error) {
	tcpListener, err := net.ListenTCP(
		"tcp",
		&net.TCPAddr{
			IP:   net.IPv4(127, 0, 0, 1),
			Port: 0,
		},
	)
	if err != nil {
		return nil, err
	}
	return plistener.New(tcpListener), nil
}

func TestPListener_MaxConn(t *testing.T) {
	t.Parallel()
	ended := false
	listener, err := getPListener()
	if err != nil {
		t.Fatal(listener)
	}
	defer func() {
		ended = true
		err = listener.Close()
		if err != nil {
			t.Fatal(err)
		}
	}()
	listener.MaxConn = 8
	raddr := listener.Addr().(*net.TCPAddr)
	go func() {
		for i := byte(1); i < 250; i++ {
			_, err := net.DialTCP(
				"tcp",
				&net.TCPAddr{
					IP:   net.IPv4(127, 0, 0, i),
					Port: 0,
				},
				raddr)
			if err != nil && !ended {
				t.Fatal(err)
			}
			time.Sleep(time.Millisecond * 10)
		}
	}()
	mut := &sync.Mutex{}
	count := 0
	reachedMax := false
	for i := 0; i < listener.MaxConn*4; i++ {
		conn, err := listener.Accept()
		if err != nil {
			t.Fatal(err)
		}
		mut.Lock()
		count++
		if count == listener.MaxConn {
			reachedMax = true
		} else if count > listener.MaxConn {
			t.Fatal("connections exceed MaxConn")
		}
		mut.Unlock()
		go func(conn net.Conn) {
			time.Sleep(time.Millisecond * 100)
			mut.Lock()
			err := conn.Close()
			if err != nil && !ended {
				t.Fatal(err)
			}
			count--
			mut.Unlock()
		}(conn)
	}
	if !reachedMax {
		t.Fatal("connections didn't reach max")
	}
}

func TestPListener_MaxConnSingleIP(t *testing.T) {
	t.Parallel()
	ended := false
	listener, err := getPListener()
	if err != nil {
		t.Fatal(listener)
	}
	defer func() {
		ended = true
		err = listener.Close()
		if err != nil {
			t.Fatal(err)
		}
	}()
	listener.MaxConnSingleIP = 8
	raddr := listener.Addr().(*net.TCPAddr)
	go func() {
		for i := 1; i < 250; i++ {
			_, err := net.DialTCP(
				"tcp",
				&net.TCPAddr{
					IP:   net.IPv4(127, 0, 0, 1),
					Port: 0,
				},
				raddr)
			if err != nil && !ended {
				t.Fatal(err)
			}
			time.Sleep(time.Millisecond * 10)
		}
	}()
	mut := &sync.Mutex{}
	count := 0
	reachedMax := false
	for i := 0; i < listener.MaxConnSingleIP*4; i++ {
		conn, err := listener.Accept()
		if err != nil {
			t.Fatal(err)
		}
		mut.Lock()
		count++
		if count == listener.MaxConnSingleIP {
			reachedMax = true
		} else if count > listener.MaxConnSingleIP {
			t.Fatal("connections exceed MaxConnSingleIP")
		}
		mut.Unlock()
		go func(conn net.Conn) {
			time.Sleep(time.Millisecond * 100)
			mut.Lock()
			err := conn.Close()
			if err != nil && !ended {
				t.Fatal(err)
			}
			count--
			mut.Unlock()
		}(conn)
	}
	if !reachedMax {
		t.Fatal("connections didn't reach max")
	}
}

func TestPListener_SetLimiter(t *testing.T) {
	t.Parallel()
	ended := false
	listener, err := getPListener()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ended = true
		err = listener.Close()
		if err != nil {
			t.Fatal(err)
		}
	}()
	raddr := listener.Addr().(*net.TCPAddr)
	listener.SetLimiter(plistener.Limiter{
		{time.Second, 3},
		{time.Second * 20, 8},
	})
	go func() {
		for i := 1; i < 2500; i++ {
			_, err := net.DialTCP(
				"tcp",
				&net.TCPAddr{
					IP:   net.IPv4(127, 0, 0, 1),
					Port: 0,
				},
				raddr)
			if err != nil && !ended {
				t.Fatal(err)
			}
			time.Sleep(time.Millisecond * 100)
		}
	}()
	for i := 0; i < 2; i++ {
		start := time.Now()
		count := 0
		err = listener.Listener.(*net.TCPListener).SetDeadline(start.Add(time.Second * 20))
		if err != nil {
			t.Fatal(err)
		}
		for {
			_, err := listener.Accept()
			if err != nil {
				nErr, ok := err.(net.Error)
				if !ok {
					t.Fatal(err)
				}
				if nErr.Timeout() {
					break
				}
				t.Fatal(err)
			}
			count++
			if time.Now().Before(start.Add(time.Second)) && count > 3 {
				t.Fatal("connections exceed the limiter")
			} else if count > 8 {
				t.Fatal("connections exceed the limiter")
			}
		}
		if count < 8 {
			t.Fatal("connections didn't reach max")
		}
		time.Sleep(time.Second * 20)
	}
}

func TestPListener_Ban(t *testing.T) {
	t.Parallel()
	ended := false
	listener, err := getPListener()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ended = true
		err = listener.Close()
		if err != nil {
			t.Fatal(err)
		}
	}()
	laddr := &net.TCPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: 0,
	}
	listener.Ban(laddr.IP)
	raddr := listener.Addr().(*net.TCPAddr)
	banned := true
	for {
		go func() {
			_, err := net.DialTCP("tcp", laddr, raddr)
			if err != nil {
				if !ended {
					t.Fatal(err)
				}
				return
			}
		}()
		err = listener.Listener.(*net.TCPListener).SetDeadline(time.Now().Add(time.Second * 7))
		if err != nil {
			log.Fatal(err)
		}
		conn, err := listener.Accept()
		if err == nil {
			if conn.RemoteAddr().(*net.TCPAddr).IP.Equal(laddr.IP) {
				if banned {
					t.Fatal("banned IP was accepted")
				}
				return
			}
			t.Fatal("hijacked")
		}
		nErr, ok := err.(net.Error)
		if !ok {
			t.Fatal("net.Error cast fialed")
		}
		if !nErr.Timeout() && banned {
			t.Fatal("expected time out error did not occur")
		}
		if !banned {
			t.Fatal(nErr)
		}
		listener.RevokeBan(laddr.IP)
		banned = false
	}
}

func TestPListener_TempBan(t *testing.T) {
	t.Parallel()
	ended := false
	listener, err := getPListener()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ended = true
		err = listener.Close()
		if err != nil {
			t.Fatal(err)
		}
	}()
	laddr := &net.TCPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: 0,
	}
	deadline := time.Now().Add(time.Second * 3)
	listener.TempBan(laddr.IP, deadline)
	raddr := listener.Addr().(*net.TCPAddr)
	go func() {
		for index := 0; index < 10; index++ {
			conn, err := net.DialTCP("tcp", laddr, raddr)
			if err != nil {
				if !ended {
					t.Fatal(err)
				}
				return
			}
			time.Sleep(time.Second)
			err = conn.Close()
			if err != nil {
				if !ended {
					t.Fatal(err)
				}
				return
			}
		}
	}()
	err = listener.Listener.(*net.TCPListener).SetDeadline(time.Now().Add(time.Minute))
	if err != nil {
		log.Fatal(err)
	}
	conn, err := listener.Accept()
	now := time.Now()
	if err != nil {
		t.Fatal(err)
	}
	if !conn.RemoteAddr().(*net.TCPAddr).IP.Equal(laddr.IP) {
		t.Fatal("hijacked")
	}
	if now.Before(deadline) {
		t.Fatal("TempBan failed")
	}
}

func TestPListener_GivePrivilege(t *testing.T) {
	t.Parallel()
	ended := false
	listener, err := getPListener()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ended = true
		err = listener.Close()
		if err != nil {
			t.Fatal(err)
		}
	}()
	raddr := listener.Addr().(*net.TCPAddr)
	laddr := &net.TCPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: 0,
	}
	listener.SetLimiter(plistener.Limiter{
		{time.Second * 5, 1},
	})
	listener.GivePrivilege(laddr.IP)
	go func() {
		for index := 0; index < 10; index++ {
			conn, err := net.DialTCP("tcp", laddr, raddr)
			if err != nil {
				if !ended {
					t.Error(err)
				}
				return
			}
			time.Sleep(time.Second)
			err = conn.Close()
			if err != nil && !ended {
				t.Fatal(err)
			}
		}
	}()
	count := 0
	for {
		err = listener.Listener.(*net.TCPListener).SetDeadline(time.Now().Add(time.Second * 7))
		if err != nil {
			log.Fatal(err)
		}
		conn, err := listener.Accept()
		if err != nil {
			nErr, ok := err.(net.Error)
			if !ok {
				t.Fatal("net.Error cast fialed")
			}
			if !nErr.Timeout() {
				t.Fatal(nErr)
			}
			if count != 6 {
				t.Fatal("GivePrivilege failed")
			}
			return
		}
		if !conn.RemoteAddr().(*net.TCPAddr).IP.Equal(laddr.IP) {
			t.Fatal("hijacked")
		}
		count++
		if count == 5 {
			listener.RevokePrivilege(laddr.IP)
		}
		if count > 6 {
			t.Fatal("GetPrivilige failed")
		}
	}
}

func TestPListener_OnSpam(t *testing.T) {
	t.Parallel()
	ended := false
	listener, err := getPListener()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ended = true
		err = listener.Close()
		if err != nil {
			t.Fatal(err)
		}
	}()
	raddr := listener.Addr().(*net.TCPAddr)
	laddr := &net.TCPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: 0,
	}
	listener.SetLimiter(plistener.Limiter{
		{time.Hour, 1},
	})
	go func() {
		for index := 0; index < 100; index++ {
			conn, err := net.DialTCP("tcp", laddr, raddr)
			if err != nil {
				if !ended {
					t.Fatal(err)
				}
				return
			}
			time.Sleep(time.Second)
			err = conn.Close()
			if err != nil {
				if !ended {
					t.Fatal(err)
				}
				return
			}
		}
	}()
	first := false
	banned := false
	listener.OnSpam = func(ip net.IP) {
		if banned {
			t.Fatal("OnSpam failed")
		}
		listener.Ban(ip)
		banned = true
	}
	for {
		err = listener.Listener.(*net.TCPListener).SetDeadline(time.Now().Add(time.Second * 5))
		if err != nil {
			log.Fatal(err)
		}
		conn, err := listener.Accept()
		if err != nil {
			nErr, ok := err.(net.Error)
			if !ok {
				t.Fatal("net.Error cast failed")
			}
			if nErr.Timeout() {
				if first && banned {
					return
				}
				t.Fatal("OnSpam failed")
			}
			t.Fatal(err)
		}
		if !conn.RemoteAddr().(*net.TCPAddr).IP.Equal(laddr.IP) {
			t.Fatal("hijacked")
		}
		if first || banned {
			t.Fatal("OnSpam failed")
		}
		first = true
	}
}
