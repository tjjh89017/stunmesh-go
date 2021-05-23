package main

import (
	"fmt"
	"net"
	"log"
	"errors"
	"time"
	"os"
	"os/exec"
	"strings"
	"strconv"
	"io"
	crypto_rand "crypto/rand"
	"crypto/sha1"
	"context"

	"encoding/binary"
	"encoding/base64"
	"encoding/hex"

	"golang.org/x/net/ipv4"
	"golang.org/x/net/bpf"
	"github.com/pion/stun"
	"github.com/cloudflare/cloudflare-go"
	"golang.org/x/crypto/nacl/box"
)

var (
	errResponseMessage = errors.New("error reading from response message channel")
	errTimedOut        = errors.New("timed out waiting for response")
	errNoOtherAddress  = errors.New("no OTHER-ADDRESS in message")
)

var (
	timeoutPtr = 5
)

type UDPHeader struct {
	SrcPort uint16
	DstPort uint16
	Length uint16
	Checksum uint16
}

type STUNSession struct {
	conn *ipv4.PacketConn
	innerConn net.PacketConn
	LocalAddr net.Addr
	LocalPort uint16
	RemoteAddr *net.UDPAddr
	OtherAddr *net.UDPAddr
	messageChan chan *stun.Message
}

func (c *STUNSession) Close() error {
	return c.conn.Close()
}

func (c *STUNSession) roundTrip(msg *stun.Message, addr net.Addr) (*stun.Message, error) {
	_ = msg.NewTransactionID()
	log.Printf("Send to %v: (%v bytes)\n", addr, msg.Length)

	send_udp := &UDPHeader {
		SrcPort: c.LocalPort,
		DstPort: uint16(c.RemoteAddr.Port),
		Length: uint16(8 + len(msg.Raw)),
		Checksum: 0,
	}

	buf := make([]byte, 8)
	binary.BigEndian.PutUint16(buf[0:], send_udp.SrcPort)
	binary.BigEndian.PutUint16(buf[2:], send_udp.DstPort)
	binary.BigEndian.PutUint16(buf[4:], send_udp.Length)
	binary.BigEndian.PutUint16(buf[6:], send_udp.Checksum)

	if _, err := c.conn.WriteTo(append(buf, msg.Raw...), nil, addr); err != nil {
		log.Fatal(err)
		return nil, err
	}

	// wait for respone
	select {
		case m, ok := <-c.messageChan:
			if!ok {
				return nil, errResponseMessage
			}
			return m, nil
		case <-time.After(time.Duration(timeoutPtr) * time.Second):
			log.Printf("time out")
			return nil, errTimedOut
	}
}

func parse(msg *stun.Message) (ret struct {
	xorAddr    *stun.XORMappedAddress
	otherAddr  *stun.OtherAddress
	mappedAddr *stun.MappedAddress
	software   *stun.Software
}) {
	ret.mappedAddr = &stun.MappedAddress{}
	ret.xorAddr = &stun.XORMappedAddress{}
	ret.otherAddr = &stun.OtherAddress{}
	ret.software = &stun.Software{}
	if ret.xorAddr.GetFrom(msg) != nil {
		ret.xorAddr = nil
	}
	if ret.otherAddr.GetFrom(msg) != nil {
		ret.otherAddr = nil
	}
	if ret.mappedAddr.GetFrom(msg) != nil {
		ret.mappedAddr = nil
	}
	if ret.software.GetFrom(msg) != nil {
		ret.software = nil
	}
	log.Printf("%v\n", msg)
	log.Printf("\tMAPPED-ADDRESS:     %v\n", ret.mappedAddr)
	log.Printf("\tXOR-MAPPED-ADDRESS: %v\n", ret.xorAddr)
	log.Printf("\tOTHER-ADDRESS:      %v\n", ret.otherAddr)
	log.Printf("\tSOFTWARE:           %v\n", ret.software)

	return ret
}

func connect(port uint16, addrStr string) (*STUNSession, error) {
	log.Printf("connecting to STUN server: %s\n", addrStr)
	addr, err := net.ResolveUDPAddr("udp4", addrStr)
	if err != nil {
		log.Fatal(err)
		return nil, err
	}

	c, err := net.ListenPacket("ip4:17", "0.0.0.0")
	if err != nil {
		log.Fatal(err)
		return nil, err
	}

	p := ipv4.NewPacketConn(c)
	// set port here
	bpf_filter, err := stun_bpf_filter(port)
	err = p.SetBPF(bpf_filter)
	if err != nil {
		log.Fatal(err)
	}

	mChan := listen(p)

	return &STUNSession {
		conn: p,
		innerConn: c,
		LocalAddr: p.LocalAddr(),
		LocalPort: port,
		RemoteAddr: addr,
		messageChan: mChan,
	}, nil

}

func listen(conn *ipv4.PacketConn) (messages chan *stun.Message) {
	messages = make(chan *stun.Message)
	go func() {
		for {
			buf := make([]byte, 1500)
			n, _, addr, err := conn.ReadFrom(buf)
			if err != nil {
				close(messages)
				return
			}
			log.Printf("Response from %v: (%v bytes)\n", addr, n)
			// cut UDP header, cut postfix
			buf = buf[8:n]

			m := new(stun.Message)
			m.Raw = buf
			err = m.Decode()
			if err != nil {
				log.Printf("Error decoding message: %v\n", err)
				close(messages)
				return
			}
			messages <- m
		}
	}()
	return
}

func stun_bpf_filter(port uint16) ([]bpf.RawInstruction, error) {
	// if possible make some magic here to determine STUN packet
	const  (
		ipOff = 0
		udpOff = ipOff + 5 * 4
		payloadOff = udpOff + 2 * 4
		stunMagicCookieOff = payloadOff + 4

		stunMagicCookie = 0x2112A442
	)
	r, e := bpf.Assemble([]bpf.Instruction{
		bpf.LoadAbsolute {
			// A = dst port
			Off: udpOff + 2,
			Size: 2,
		},
		bpf.JumpIf {
			// if A == `port`
			Cond: bpf.JumpEqual,
			Val: uint32(port),
			SkipFalse: 3,
		},
		bpf.LoadAbsolute {
			// A = stun magic part
			Off: stunMagicCookieOff,
			Size: 4,
		},
		bpf.JumpIf {
			// if A == stun magic value
			Cond: bpf.JumpEqual,
			Val: stunMagicCookie,
			SkipFalse: 1,
		},
		// we need
		bpf.RetConstant {
			Val: 262144,
		},
		// port and stun are not we need
		bpf.RetConstant {
			Val: 0,
		},
	})
	if e != nil {
		log.Fatal(e)
	}
	return r, e
}

func main() {
	fmt.Println("Hello")

	// read config from env
	WG := os.Getenv("WG")
	CF_API_KEY := os.Getenv("CF_API_KEY")
	CF_API_EMAIL := os.Getenv("CF_API_EMAIL")
	CF_ZONE_NAME := os.Getenv("CF_ZONE_NAME")

	// get wg setting
	raw_data, err := exec.Command("wg", "show", WG, "dump").Output()
	raw_data_slice := strings.Split(string(raw_data), "\n")
	local_data := strings.Fields(raw_data_slice[0])
	LocalPrivateKey := local_data[0]
	var LocalPrivateKeyBytes [32]byte
	LocalPublicKey := local_data[1]
	var LocalPublicKeyBytes [32]byte
	LocalListenPort, err := strconv.ParseInt(local_data[2], 10, 16)
	if err != nil {
		log.Fatal(err)
	}
	LocalPrivateKeyBytePtr, err := base64.StdEncoding.DecodeString(LocalPrivateKey)
	if err != nil {
		log.Fatal(err)
	}
	copy(LocalPrivateKeyBytes[:], LocalPrivateKeyBytePtr)
	LocalPublicKeyBytePtr, err := base64.StdEncoding.DecodeString(LocalPublicKey)
	if err != nil {
		log.Fatal(err)
	}
	copy(LocalPublicKeyBytes[:], LocalPublicKeyBytePtr)

	// assume we only have one peer
	// FIXME
	remote_data := strings.Fields(raw_data_slice[1])
	RemotePublicKey := remote_data[0]
	var RemotePublicKeyBytes [32]byte
	RemoteEndpoint := remote_data[2]
	RemotePublicKeyBytePtr, err := base64.StdEncoding.DecodeString(RemotePublicKey)
	if err != nil {
		log.Fatal(err)
	}
	copy(RemotePublicKeyBytes[:], RemotePublicKeyBytePtr)

	Conn, err := connect(uint16(LocalListenPort) ,"stun.l.google.com:19302")
	defer Conn.Close()
	if err != nil {
		log.Fatal(err)
	}

	request := stun.MustBuild(stun.TransactionID, stun.BindingRequest)
	response_data, err := Conn.roundTrip(request, Conn.RemoteAddr)
	if err != nil {
		log.Fatal(err)
	}

	response := parse(response_data)
	if response.xorAddr != nil {
		log.Printf("addr: %s\n", response.xorAddr.String())
	} else {
		log.Printf("error no xor addr")
	}

	// prepare sealedbox for storage
	var nonce [24]byte
	if _, err := io.ReadFull(crypto_rand.Reader, nonce[:]); err != nil {
		log.Fatal(err)
	}
	log.Printf("nonce: %s\n", hex.EncodeToString(nonce[:]))
	// msg = public ip and port
	msg := []byte(response.xorAddr.String())
	encryptedData := box.Seal(nonce[:], msg, &nonce, &RemotePublicKeyBytes, &LocalPrivateKeyBytes)
	log.Printf("encryptedData: %s\n", hex.EncodeToString(encryptedData))

	// prepare domain for storing
	// sha1(From..To)
	sha1DomainSlice := sha1.Sum(append(LocalPublicKeyBytePtr, RemotePublicKeyBytePtr...))
	sha1Domain := ""
	for _, i := range sha1DomainSlice {
		sha1Domain = sha1Domain + fmt.Sprintf("%02x", i)
	}
	log.Printf("sha1: %s\n", sha1Domain)


	// prepare save to CloudFlare
	CFApi, err := cloudflare.New(CF_API_KEY, CF_API_EMAIL)
	if err != nil {
		log.Fatal(err)
	}
	// Fetch zone id
	ZoneID, err := CFApi.ZoneIDByName(CF_ZONE_NAME)
	if err != nil {
		log.Fatal(err)
	}

	// fetch dns record id
	records, err := CFApi.DNSRecords(context.Background(), ZoneID, cloudflare.DNSRecord{ Type: "TXT", Name: sha1Domain + "." + CF_ZONE_NAME })
	if err != nil {
		log.Fatal(err)
	}
	for _, r := range records {
		log.Printf("%s: %s\n", r.Name, r.Content)
	}

	record := cloudflare.DNSRecord {
		Type: "TXT",
		Name: sha1Domain + "." + CF_ZONE_NAME,
		TTL: 1,
		Content: hex.EncodeToString(encryptedData),
	}
	// if record empty
	if len(records) == 0 {
		// create it
		if _, err := CFApi.CreateDNSRecord(context.Background(), ZoneID, record); err != nil {
			log.Fatal(err)
		}
	} else {
		// Update it
		// TODO if data is same, don't update it
		recordID := records[0].ID
		if err := CFApi.UpdateDNSRecord(context.Background(), ZoneID, recordID, record); err != nil {
			log.Fatal(err)
		}
		if len(records) > 1 {
			for _, x := range records[1:] {
				if err := CFApi.DeleteDNSRecord(context.Background(), ZoneID, x.ID); err != nil {
					log.Fatal(err)
				}
			}
		}
	}

	// get record from remote peer to update peer endpoint
	// prepare domain to get
	sha1DomainSlice = sha1.Sum(append(RemotePublicKeyBytePtr, LocalPublicKeyBytePtr...))
	sha1Domain = ""
	for _, i := range sha1DomainSlice {
		sha1Domain = sha1Domain + fmt.Sprintf("%02x", i)
	}
	log.Printf("sha1: %s\n", sha1Domain)
	// fetch dns records
	records, err = CFApi.DNSRecords(context.Background(), ZoneID, cloudflare.DNSRecord{ Type: "TXT", Name: sha1Domain + "." + CF_ZONE_NAME })
	if err != nil {
		log.Fatal(err)
	}
	for _, r := range records {
		log.Printf("%s: %s\n", r.Name, r.Content)
	}
	if len(records) == 0 {
		log.Printf("no record found\n")
		return
	}
	record = records[0]
	encryptedData, err = hex.DecodeString(record.Content)
	if err != nil {
		log.Fatal(err)
	}
	var decryptedNonce [24]byte
	copy(decryptedNonce[:], encryptedData[:24])
	decryptedData, ok := box.Open(nil, encryptedData[24:], &decryptedNonce, &RemotePublicKeyBytes, &LocalPrivateKeyBytes)
	if !ok {
		log.Fatal("err")
	}
	log.Printf("%s", decryptedData)

	// ready to setup endpoint
	raw_data, err = exec.Command("wg", "set", WG, "peer", RemotePublicKey, "endpoint", string(decryptedData)).Output()

	_ = raw_data
	_ = RemoteEndpoint
}
