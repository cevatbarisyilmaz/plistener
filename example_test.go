package plistener_test

import (
	"github.com/cevatbarisyilmaz/plistener"
	"log"
	"net"
	"net/http"
	"time"
)

func Example() {
	tcpAddr := &net.TCPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: 80,
	}
	//Create the TCP listener
	tcpListener, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		log.Fatal(err)
	}
	//Create a PListener via the TCP listener
	pListener := plistener.New(tcpListener)
	for {
		//Accept the next connection
		conn, err := pListener.Accept()
		if err != nil {
			//Handle the error
			log.Fatal(err)
		}
		//Start a new goroutine to handle the connection
		go func() {
			//Write hi to the other side and close the connection
			_, err := conn.Write([]byte("Hi!"))
			if err != nil {
				log.Fatal(err)
			}
			err = conn.Close()
			if err != nil {
				log.Fatal(err)
			}
		}()
	}
}

func Example_hTTP() {
	tcpAddr := &net.TCPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: 80,
	}
	//Create the TCP listener
	tcpListener, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		log.Fatal(err)
	}
	//Create a PListener via the TCP listener
	pListener := plistener.New(tcpListener)
	//Initialize a HTTP server
	server := &http.Server{}
	//Serve HTTP over PListener
	err = server.Serve(pListener)
	//Handle the error
	log.Fatal(err)
}

func Example_advanced() {
	tcpAddr := &net.TCPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: 80,
	}
	//Create the TCP listener
	tcpListener, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		log.Fatal(err)
	}
	//Create a PListener via the TCP listener
	pListener := plistener.New(tcpListener)
	//Set number of maximum alive connections to 16
	pListener.MaxConn = 16
	//Set number of maximum alive connections with a single IP to 2
	pListener.MaxConnSingleIP = 2
	//Let remote IP addresses to create only 2 new connections in a minute and only 20 new connections in an hour
	pListener.SetLimiter(plistener.Limiter{
		{time.Minute, 2},
		{time.Hour, 20},
	})
	//If an IP exceeds the rate limit or maximum amount of connections limit, ignore all the new requests of it for a day.
	pListener.OnSpam = func(ip net.IP) {
		pListener.TempBan(ip, time.Now().Add(time.Hour*24))
	}
	//Give privilege to the localhost to bypass all the limits
	pListener.GivePrivilege(net.IPv4(127, 0, 0, 1))
	for {
		//Wait for a new connection
		conn, err := pListener.Accept()
		if err != nil {
			log.Fatal(err)
		}
		//Handle the connection
		go func() {
			//Write hi to the other side and close the connection
			_, err := conn.Write([]byte("Hi!"))
			if err != nil {
				log.Fatal(err)
			}
			err = conn.Close()
			if err != nil {
				log.Fatal(err)
			}
		}()
	}
}
