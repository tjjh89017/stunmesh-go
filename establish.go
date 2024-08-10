package main

import (
	"context"
	"log"
	"net"

	"github.com/cloudflare/cloudflare-go"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func establishPeers(
	ctrl *wgctrl.Client,
	device *wgtypes.Device,
	peer *wgtypes.Peer,
	decryptor Decryptor,
	cfApi *cloudflare.API,
	zoneId string,
	zoneName string,
) {
	// get record from remote peer to update peer endpoint
	// prepare domain to get
	sha1Domain := buildExchangeKey(peer.PublicKey[:], device.PublicKey[:])
	log.Printf("sha1: %s\n", sha1Domain)
	// fetch dns records
	records, err := cfApi.DNSRecords(context.Background(), zoneId, cloudflare.DNSRecord{Type: "TXT", Name: sha1Domain + "." + zoneName})
	if err != nil {
		log.Panic(err)
	}

	for _, r := range records {
		log.Printf("%s: %s\n", r.Name, r.Content)
	}

	if len(records) == 0 {
		log.Printf("no record found\n")
		return
	}

	record := records[0]
	host, port, err := decryptor.Decrypt(record.Content)
	if err != nil {
		log.Panic(err)
	}

	err = ctrl.ConfigureDevice(device.Name, wgtypes.Config{
		Peers: []wgtypes.PeerConfig{
			{
				PublicKey:  peer.PublicKey,
				UpdateOnly: true,
				Endpoint: &net.UDPAddr{
					IP:   net.ParseIP(host),
					Port: port,
				},
			},
		},
	})

	if err != nil {
		log.Panic(err)
	}
}
