package plistener

import (
	"net"
	"strconv"
	"sync"
	"testing"
	"time"
)

var port = 999

func getPListener() (*PListener, error) {
	port++
	addr := net.JoinHostPort("127.0.0.2", strconv.Itoa(port))
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

func TestMaxConn(t *testing.T) {
	listener, err := getPListener()
	if err != nil {
		t.Fatal(listener)
	}
	defer func() {
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
					t.Fatal(err)
				}
				net.DialTCP("tcp", tcpAddr, raddr)
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
	listener, err := getPListener()
	if err != nil {
		t.Fatal(listener)
	}
	defer func() {
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
				addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(i))
				tcpAddr, err := net.ResolveTCPAddr("tcp", addr)
				if err != nil {
					t.Fatal(err)
				}
				net.DialTCP("tcp", tcpAddr, raddr)
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

func TestLimits(t *testing.T) {
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
	listener.SetLimits([]struct {
		time.Duration
		int
	}{
		{time.Second, 4},
		{time.Minute, 8},
	})
	for i := 0; i < 5; i++ {
		start := time.Now()
		go func() {
			for i := 1; i < 5001; i++ {
				time.Sleep(time.Millisecond * 10)
				laddr, err := net.ResolveTCPAddr("tcp", net.JoinHostPort("127.0.0.5", strconv.Itoa(i)))
				if err != nil {
					t.Fatal(err)
				}
				net.DialTCP("tcp", laddr, addr)
			}
		}()
		go func() {
			count := 0
			defer func() {
				if count != 8 {
					t.Fatal("too few connections")
				}
			}()
			for {
				listener.TCPListener.SetDeadline(start.Add(time.Minute))
				conn, err := listener.Accept()
				if err != nil {
					nErr, ok := err.(net.Error)
					if !ok {
						t.Fatal("net.Error cast failed")
					}
					if !nErr.Timeout() {
						t.Fatal(nErr)
					}
					break
				}
				count++
				if time.Now().Before(start.Add(time.Second)) {
					if count > 4 {
						t.Fatal("accepted connections exceed the limits")
					}
				}
				if count > 8 {
					t.Fatal("accepted connections exceed the limits")
				}
				conn.Close()
			}
		}()
		time.Sleep(time.Minute + time.Second)
	}
}

func TestIPBan(t *testing.T) {
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
	laddr, err := net.ResolveTCPAddr("tcp", "127.0.0.3:999")
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
				t.Fatal(err)
			}
		}()
		listener.TCPListener.SetDeadline(time.Now().Add(time.Second * 7))
		conn, err := listener.Accept()
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
