package integration

import (
	"net"
)

// freeport - find a free TCP port for immediate use. No guarantees!
func freeport() (port int, addr string) {
	l, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		panic(err)
	}
	defer l.Close()
	a := l.Addr().(*net.TCPAddr)
	port = a.Port

	return port, a.String()
}
