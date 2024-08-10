package main

import (
	"context"
	"encoding/hex"
	"log"
	"net"
	"strconv"

	"github.com/cloudflare/cloudflare-go"
	"golang.org/x/crypto/nacl/box"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func establishPeers(
	ctrl *wgctrl.Client,
	device *wgtypes.Device,
	peer *wgtypes.Peer,
	cfApi *cloudflare.API,
	zoneId string,
	zoneName string,
	remotePublicKey [32]byte,
	localPrivateKey [32]byte,
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
	encryptedData, err := hex.DecodeString(record.Content)
	if err != nil {
		log.Panic(err)
	}
	var decryptedNonce [24]byte
	copy(decryptedNonce[:], encryptedData[:24])

	decryptedData, ok := box.Open(nil, encryptedData[24:], &decryptedNonce, &remotePublicKey, &localPrivateKey)
	if !ok {
		log.Panic("err")
	}
	log.Printf("%s", decryptedData)

	// ready to setup endpoint
	host, port, err := net.SplitHostPort(string(decryptedData))
	if err != nil {
		log.Panic(err)
	}

	intPort, err := strconv.Atoi(port)
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
					Port: intPort,
				},
			},
		},
	})

	if err != nil {
		log.Panic(err)
	}
}
