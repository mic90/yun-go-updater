package main

import (
	"net"
	"time"

	"github.com/pkg/errors"
)

func externalIP(notThis string) (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue // interface down
		}
		if iface.Flags&net.FlagLoopback != 0 {
			continue // loopback interface
		}
		addrs, err := iface.Addrs()
		if err != nil {
			return "", err
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			ip = ip.To4()
			if ip == nil || ip.String() == notThis {
				continue // not an ipv4 address
			}
			return ip.String(), nil
		}
	}
	return "", errors.New("are you connected to the network?")
}

// GetServerAndBoardIP sets pointers given in arguments to the own ip address and the board ip address
func GetServerAndBoardIP(serverAddr, ipAddr *string) error {
	// get self ip addresses
	var err error
	*serverAddr, err = externalIP(*serverAddr)
	if err != nil {
		return errors.Wrap(err, "could not obtain own IP address")
	}
	// remove last octect to get an available IP adress for the board
	ip := net.ParseIP(*serverAddr)
	ip = ip.To4()
	// start trying from server IP + 1
	ip[3] = 24
	for ip[3] < 255 {
		_, err := net.DialTimeout("tcp", ip.String(), 2*time.Second)
		if err != nil {
			break
		}
		ip[3]++
	}
	*ipAddr = ip.String()
	return nil
}
