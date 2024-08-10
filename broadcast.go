package main

import (
	"context"
	crypto_rand "crypto/rand"
	"encoding/hex"
	"io"
	"log"

	"github.com/cloudflare/cloudflare-go"
	"github.com/pion/stun"
	"golang.org/x/crypto/nacl/box"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func broadcastPeers(
	device *wgtypes.Device,
	peer *wgtypes.Peer,
	cfApi *cloudflare.API,
	zoneName string,
	zoneId string,
	remotePublicKey [32]byte,
	localPrivateKey [32]byte,
) {
	// get wg setting

	conn, err := connect(uint16(device.ListenPort), "stun.l.google.com:19302")
	if err != nil {
		log.Panic(err)
	}
	defer conn.Close()

	request := stun.MustBuild(stun.TransactionID, stun.BindingRequest)
	resData, err := conn.roundTrip(request, conn.RemoteAddr)
	if err != nil {
		log.Panic(err)
	}

	response := parse(resData)
	if response.xorAddr != nil {
		log.Printf("addr: %s\n", response.xorAddr.String())
	} else {
		log.Printf("error no xor addr")
	}

	// prepare sealedbox for storage
	var nonce [24]byte
	if _, err := io.ReadFull(crypto_rand.Reader, nonce[:]); err != nil {
		log.Panic(err)
	}
	log.Printf("nonce: %s\n", hex.EncodeToString(nonce[:]))
	// msg = public ip and port
	msg := []byte(response.xorAddr.String())
	encryptedData := box.Seal(nonce[:], msg, &nonce, &remotePublicKey, &localPrivateKey)
	log.Printf("encryptedData: %s\n", hex.EncodeToString(encryptedData))

	// prepare domain for storing
	// sha1(From..To)
	sha1Domain := buildExchangeKey(device.PublicKey[:], peer.PublicKey[:])
	log.Printf("sha1: %s\n", sha1Domain)

	// fetch dns record id
	records, err := cfApi.DNSRecords(context.Background(), zoneId, cloudflare.DNSRecord{Type: "TXT", Name: sha1Domain + "." + zoneName})
	if err != nil {
		log.Panic(err)
	}
	for _, r := range records {
		log.Printf("%s: %s\n", r.Name, r.Content)
	}

	record := cloudflare.DNSRecord{
		Type:    "TXT",
		Name:    sha1Domain + "." + zoneName,
		TTL:     1,
		Content: hex.EncodeToString(encryptedData),
	}
	// if record empty
	if len(records) == 0 {
		// create it
		if _, err := cfApi.CreateDNSRecord(context.Background(), zoneId, record); err != nil {
			log.Panic(err)
		}
	} else {
		// Update it
		// TODO if data is same, don't update it
		recordID := records[0].ID
		if err := cfApi.UpdateDNSRecord(context.Background(), zoneId, recordID, record); err != nil {
			log.Panic(err)
		}
		if len(records) > 1 {
			for _, x := range records[1:] {
				if err := cfApi.DeleteDNSRecord(context.Background(), zoneId, x.ID); err != nil {
					log.Panic(err)
				}
			}
		}
	}
}
