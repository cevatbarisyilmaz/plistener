package plistener

import (
	"net"
	"strconv"
	"sync"
	"testing"
	"time"
)

var port = 999
var portMutex = &sync.Mutex{}

func getPListener() (*PListener, error) {
	portMutex.Lock()
	myPort := port
	port++
	portMutex.Unlock()
	addr := net.JoinHostPort("127.0.254.1", strconv.Itoa(myPort))
	tcpAddress, err := net.ResolveTCPAddr("tcp", addr)
	if err != nil {
		return nil, err
	}
	tcpListener, err := net.ListenTCP("tcp", tcpAddress)
	if err != nil {
		return nil, err
	}
	return New(tcpListener), nil
}

func TestPListener_AcceptTimeout(t *testing.T) {
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
	laddr, err := net.ResolveTCPAddr("tcp", "127.0.1.0:999")
	if err != nil {
		t.Fatal(err)
	}
	raddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatal("*net.TCPAddr cast failed")
	}
	go func() {
		conn, err := net.DialTCP("tcp", laddr, raddr)
		if err != nil {
			if !ended {
				t.Error(err)
			}
			return
		}
		err = conn.Close()
		if err != nil {
			if !ended {
				t.Error(err)
			}
			return
		}
	}()
	conn, err := listener.AcceptTimeout(time.Second * 5)
	if err != nil {
		t.Fatal(err)
	}
	if conn.RemoteAddr().String() != laddr.String() {
		t.Fatal("hijacked")
	}
	conn, err = listener.AcceptTimeout(time.Second * 5)
	if err == nil {
		t.Fatal("Timeout failed")
	}
	nErr, ok := err.(net.Error)
	if !ok {
		t.Fatal("Timeout failed")
	}
	if !nErr.Timeout() {
		t.Fatal("Timeout failed")
	}
	go func() {
		laddr.Port++
		conn, err := net.DialTCP("tcp", laddr, raddr)
		if err != nil {
			if !ended {
				t.Error(err)
			}
			return
		}
		err = conn.Close()
		if err != nil {
			if !ended {
				t.Error(err)
			}
			return
		}
	}()
	conn, err = listener.AcceptTimeout(time.Second * 5)
	if err != nil {
		t.Fatal(err)
	}
	if conn.RemoteAddr().String() != laddr.String() {
		t.Fatal("hijacked")
	}
}

func TestMaxConn(t *testing.T) {
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
	raddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatal("*net.TCPAddr cast failed")
	}
	go func() {
		port := 2000
		for i := 2; i < 642; i++ {
			go func(i, port int) {
				ip := "127.0.0." + strconv.Itoa((i%250)+2)
				addr := net.JoinHostPort(ip, strconv.Itoa(port))
				tcpAddr, err := net.ResolveTCPAddr("tcp", addr)
				if err != nil {
					if !ended {
						t.Error(err)
					}
					return
				}
				_, err = net.DialTCP("tcp", tcpAddr, raddr)
				if err != nil {
					if !ended {
						t.Error(err)
					}
					return
				}
			}(i, port)
			port++
			time.Sleep(time.Millisecond * 100)
		}
	}()
	mut := &sync.Mutex{}
	count := 0
	for i := 0; i < 32; i++ {
		conn, err := listener.Accept()
		if err != nil {
			t.Fatal(err)
		}
		mut.Lock()
		count++
		if count > listener.MaxConn {
			t.Fatal("connections exceed MaxConn")
		}
		mut.Unlock()
		go func(conn net.Conn) {
			time.Sleep(time.Second)
			mut.Lock()
			conn.Close()
			count--
			mut.Unlock()
		}(conn)
	}
}

func TestMaxConnSingleIP(t *testing.T) {
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
	raddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatal("*net.TCPAddr cast failed")
	}
	go func() {
		for i := 2000; i < 20420; i++ {
			go func(i int) {
				addr := net.JoinHostPort("127.0.1.1", strconv.Itoa(i))
				tcpAddr, err := net.ResolveTCPAddr("tcp", addr)
				if err != nil {
					if !ended {
						t.Error(err)
					}
					return
				}
				_, err = net.DialTCP("tcp", tcpAddr, raddr)
				if err != nil {
					if !ended {
						t.Error(err)
					}
					return
				}
			}(i)
			time.Sleep(time.Millisecond * 100)
		}
	}()
	mut := &sync.Mutex{}
	count := 0
	for i := 0; i < 32; i++ {
		conn, err := listener.Accept()
		if err != nil {
			t.Fatal(err)
		}
		mut.Lock()
		count++
		if count > listener.MaxConnSingleIP {
			t.Fatal("connections exceed MaxConnSingleIP")
		}
		mut.Unlock()
		go func(conn net.Conn) {
			time.Sleep(time.Second)
			mut.Lock()
			conn.Close()
			count--
			mut.Unlock()
		}(conn)
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
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatal("*net.TCPAddr cast failed")
	}
	listener.SetLimiter(Limiter{
		{time.Second, 4},
		{time.Second * 20, 8},
	})
	for i := 0; i < 3; i++ {
		start := time.Now()
		go func(c int) {
			for i := 1021 + c*6021; i < 6021+c*6021; i++ {
				time.Sleep(time.Millisecond * 10)
				laddr, err := net.ResolveTCPAddr("tcp", net.JoinHostPort("127.0.1.2", strconv.Itoa(i)))
				if err != nil {
					if !ended {
						t.Error(err)
					}
					return
				}
				conn, err := net.DialTCP("tcp", laddr, addr)
				if err == nil {
					conn.Close()
				}
			}
		}(i)
		go func() {
			count := 0
			defer func() {
				if count != 8 && !ended {
					t.Error("too few connections")
					return
				}
			}()
			for {
				conn, err := listener.AcceptTimeout(time.Second * 20)
				if err != nil {
					nErr, ok := err.(net.Error)
					if !ok {
						if !ended {
							t.Error("net.Error cast failed")
						}
						return
					}
					if !nErr.Timeout() {
						if !ended {
							t.Error(nErr)
						}
						return
					}
					break
				}
				count++
				if time.Now().Before(start.Add(time.Second)) {
					if count > 4 {
						if !ended {
							t.Error("accepted connections exceed the limiter")
						}
						return
					}
				}
				if count > 8 {
					if !ended {
						t.Error("accepted connections exceed the limiter")
					}
					return
				}
				conn.Close()
			}
		}()
		time.Sleep(2*time.Minute + time.Second)
	}
	time.Sleep(time.Second * 41)
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
	laddr, err := net.ResolveTCPAddr("tcp", "127.0.1.3:999")
	if err != nil {
		t.Fatal(err)
	}
	listener.Ban(laddr.IP)
	raddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatal("*net.TCPAddr cast failed")
	}
	banned := true
	for {
		go func() {
			laddr.Port++
			_, err := net.DialTCP("tcp", laddr, raddr)
			if err != nil {
				if !ended {
					t.Error(err)
				}
				return
			}
		}()
		conn, err := listener.AcceptTimeout(time.Second * 7)
		if err == nil {
			if conn.RemoteAddr().String() == laddr.String() {
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
	laddr, err := net.ResolveTCPAddr("tcp", "127.0.1.4:999")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Minute)
	listener.TempBan(laddr.IP, deadline)
	raddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatal("*net.TCPAddr cast failed")
	}
	go func() {
		for index := 0; index < 10; index++ {
			conn, err := net.DialTCP("tcp", laddr, raddr)
			if err != nil {
				if !ended {
					t.Error(err)
				}
				return
			}
			time.Sleep(time.Second * 10)
			err = conn.Close()
			if err != nil {
				if !ended {
					t.Error(err)
				}
				return
			}
		}
	}()
	conn, err := listener.AcceptTimeout(time.Minute * 2)
	now := time.Now()
	if err != nil {
		t.Fatal(err)
	}
	if conn.RemoteAddr().String() != laddr.String() {
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
	raddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatal("*net.TCPAddr cast failed")
	}
	laddr, err := net.ResolveTCPAddr("tcp", "127.0.1.5:999")
	if err != nil {
		t.Fatal(err)
	}
	listener.SetLimiter(Limiter{
		{time.Minute * 2, 1},
	})
	listener.GivePrivilege(laddr.IP)
	go func() {
		for index := 0; index < 10; index++ {
			laddr.Port++
			conn, err := net.DialTCP("tcp", laddr, raddr)
			if err != nil {
				if !ended {
					t.Error(err)
				}
				return
			}
			time.Sleep(time.Second)
			conn.Close()
		}
	}()
	count := 0
	for {
		conn, err := listener.AcceptTimeout(time.Second * 10)
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
		if conn.RemoteAddr().String() != laddr.String() {
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

func TestOnSpam(t *testing.T) {
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
	raddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatal("*net.TCPAddr cast failed")
	}
	laddr, err := net.ResolveTCPAddr("tcp", "127.0.1.6:999")
	if err != nil {
		t.Fatal(err)
	}
	listener.SetLimiter(Limiter{
		{time.Hour, 1},
	})
	go func() {
		for index := 0; index < 100; index++ {
			laddr.Port++
			conn, err := net.DialTCP("tcp", laddr, raddr)
			if err != nil {
				if !ended {
					t.Error(err)
				}
				return
			}
			time.Sleep(time.Second)
			err = conn.Close()
			if err != nil {
				if !ended {
					t.Error(err)
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
		conn, err := listener.AcceptTimeout(time.Second * 5)
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
		if conn.RemoteAddr().String() != laddr.String() {
			t.Fatal("hijacked")
		}
		if first || banned {
			t.Fatal("OnSpam failed")
		}
		first = true
	}
}
