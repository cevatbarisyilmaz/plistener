# plistener

plistener is a spam-resistant TCP listener package for Go.

## Download

```
go get github.com/cevatbarisyilmaz/plistener
```

## Usage

TCP Sockets
```go
//Create a PListener from a TCP Listener
listener := plistener.New(tcpListener)
for {
	//Start accepting connections
	TCPConn, err := listener.Accept()
	if err != nil{
		//Handle error
	}
	go handleConn(TCPConn) //Handle conn
}
```

HTTP Server
```go
//Create a PListener from a TCP Listener
listener := plistener.New(tcpListener)
//Init the server
server := &http.Server{}
//Serve HTTP over PListener
server.Serve(listener)
```

## Documentation

See [godoc](https://godoc.org/github.com/cevatbarisyilmaz/plistener).

## Features

- [x] Maximum Total Open Connections Limit
- [ ] Maximum Open Connections With a Single IP Limit
- [x] Maximum New Connections With a Single IP in a Time Interval

## Contributing

PRs welcome.

## License

[MIT](https://github.com/cevatbarisyilmaz/plistener/blob/master/LICENSE)
