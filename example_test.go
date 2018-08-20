package plistener_test

import (
	"github.com/cevatbarisyilmaz/plistener"
	"log"
	"net"
	"net/http"
	"time"
)

func Example() {
	//Resolve the TCP address
	tcpAddr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:80")
	if err != nil {
		log.Fatalln(err)
	}
	//Create the TCP listener
	tcpListener, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		log.Fatalln(err)
	}
	//Create a PListener via the TCP listener
	pListener := plistener.New(tcpListener)
	for {
		//Accept the next connection
		conn, err := pListener.Accept()
		if err != nil {
			//Handle the error
			log.Fatalln(err)
		}
		//Start a new goroutine to handle the connection
		go func() {
			//Write hi to the other side and close the connection
			conn.Write([]byte("Hi!"))
			conn.Close()
		}()
	}
}

func Example_hTTP() {
	//Resolve the TCP address
	tcpAddr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:80")
	if err != nil {
		log.Fatalln(err)
	}
	//Create the TCP listener
	tcpListener, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		log.Fatalln(err)
	}
	//Create a PListener via the TCP listener
	pListener := plistener.New(tcpListener)
	//Initialize a HTTP server
	server := &http.Server{}
	//Serve HTTP over PListener
	server.Serve(pListener)
}

func Example_advanced() {
	//Resolve the TCP address
	tcpAddr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:80")
	if err != nil {
		log.Fatalln(err)
	}
	//Create the TCP listener
	tcpListener, err := net.ListenTCP("tcp", tcpAddr)
	if err != nil {
		log.Fatalln(err)
	}
	//Create a PListener via the TCP listener
	pListener := plistener.New(tcpListener)
	//Set number of maximum alive connections to 16
	pListener.MaxConn = 16
	//Set number of maximum alive connections with a single IP to 2
	pListener.MaxConnSingleIP = 2
	//Let IP addresses to create only 2 new connections in minute and only 20 new connections in an hour
	pListener.SetLimiter(plistener.Limiter{
		{time.Minute, 2},
		{time.Hour, 20},
	})
	//If an IP exceeds the rate limit or maximum amount of connections limit, ignore all the new requests for a day.
	pListener.OnSpam = func(ip net.IP) {
		pListener.TempBan(ip, time.Now().Add(time.Hour*24))
	}
	//Give privilege to the localhost to bypass all the limits
	localIPAddr, err := net.ResolveIPAddr("ip", "127.0.0.1")
	if err != nil {
		log.Fatalln(err)
	}
	pListener.GivePrivilege(localIPAddr.IP)
	for {
		//Wait for a new connection for a minute
		conn, err := pListener.AcceptTimeout(time.Minute)
		if err != nil {
			//Check if the error is actually about timeout
			nErr, ok := err.(net.Error)
			if !ok {
				log.Fatalln(err)
			}
			if !nErr.Timeout() {
				log.Fatalln(nErr)
			}
			//Log the lack of connectivity and keep accepting new connections
			log.Println("No new connections over the last minute")
			continue
		}
		//Handle the connection
		go func() {
			conn.Write([]byte("Hi!"))
			conn.Close()
		}()
	}
}
