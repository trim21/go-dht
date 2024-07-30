package dht

import (
	"net"
)

var defaultBootstrapAddress = []string{
	"router.utorrent.com:6881",
	"router.bittorrent.com:6881",
	"dht.transmissionbt.com:6881",
	"dht.aelitis.com:6881",
	"router.silotis.us:6881",
	"dht.libtorrent.org:25401",
	"dht.anacrolix.link:42069",
	"router.bittorrent.cloud:42069",
}

func (d *DHT) Bootstrap() {
	for _, addr := range defaultBootstrapAddress {
		a, err := net.ResolveUDPAddr("udp", addr)
		if err != nil {
			continue
		}

		d.AddNode(a.String())
	}
}
