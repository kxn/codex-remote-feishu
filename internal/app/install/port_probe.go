package install

import (
	"net"
	"strconv"
)

var instancePortAvailableFunc = checkInstancePortAvailable

func portSetAvailable(ports instancePortSet) bool {
	for _, port := range []int{
		ports.Relay,
		ports.Admin,
		ports.Tool,
		ports.ExternalAccess,
		ports.Pprof,
	} {
		if !instancePortAvailableFunc(port) {
			return false
		}
	}
	return true
}

func checkInstancePortAvailable(port int) bool {
	if port <= 0 {
		return false
	}
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = listener.Close()
	return true
}
