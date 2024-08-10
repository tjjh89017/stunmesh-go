package main

import (
	"context"
	"log"

	"github.com/cloudflare/cloudflare-go"
	"github.com/pion/stun"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func broadcastPeers(
	device *wgtypes.Device,
	peer *wgtypes.Peer,
	serializer Serializer,
	cfApi *cloudflare.API,
	zoneName string,
	zoneId string,
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

	endpointData, err := serializer.Serialize(response.xorAddr.IP.String(), response.xorAddr.Port)
	if err != nil {
		log.Panic(err)
	}

	// prepare domain for storing
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
		Content: endpointData,
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
