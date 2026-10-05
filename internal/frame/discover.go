package frame

import (
	"context"
	"strings"

	"github.com/libp2p/zeroconf/v2"
)

const devkitService = "_steamos-devkit._tcp"

// Discover reports every devkit service on the LAN until ctx ends.
func Discover(ctx context.Context, found func(Headset)) error {
	entries := make(chan *zeroconf.ServiceEntry, 8)
	go func() {
		for e := range entries {
			h := Headset{Host: strings.TrimSuffix(e.HostName, "."), Login: "steamos"}
			if len(e.AddrIPv4) > 0 {
				h.Addr = e.AddrIPv4[0].String()
			}
			for _, txt := range e.Text {
				if v, ok := strings.CutPrefix(txt, "login="); ok {
					h.Login = v
				}
			}
			found(h)
		}
	}()
	return zeroconf.Browse(ctx, devkitService, "local.", entries)
}
