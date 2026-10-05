package frame

import "net"

const devkitPort = "32000"

type Headset struct {
	Host  string // hostname or host:port as the user or mDNS gave it
	Addr  string // IP when known
	Login string // ssh user from /login-name
}

// devkitURL keeps an explicit port if the host carries one (tests), else uses Valve's.
func devkitURL(host, path string) string {
	if _, _, err := net.SplitHostPort(host); err != nil {
		host = net.JoinHostPort(host, devkitPort)
	}
	return "http://" + host + path
}

func (h Headset) sshAddr() string {
	host := h.Host
	if h.Addr != "" {
		host = h.Addr
	}
	if hostOnly, _, err := net.SplitHostPort(host); err == nil {
		host = hostOnly
	}
	return net.JoinHostPort(host, "22")
}
