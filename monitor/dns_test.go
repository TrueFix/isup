package monitor

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

func TestDNSFallbackToTCP(t *testing.T) {
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer tcp.Close()
	udp, err := net.ListenPacket("udp", tcp.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	udpFinished, tcpFinished := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(udpFinished)
		buffer := make([]byte, 512)
		n, addr, err := udp.ReadFrom(buffer)
		if err != nil {
			return
		}
		var query dnsmessage.Message
		if query.Unpack(buffer[:n]) != nil {
			return
		}
		// Header-only TC response, with a missing question/answer body.
		query.Response, query.Truncated = true, true
		packet, _ := query.Pack()
		udp.WriteTo(packet[:12], addr)
	}()
	go func() {
		defer close(tcpFinished)
		conn, err := tcp.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(2 * time.Second))
		var size [2]byte
		if _, err := io.ReadFull(conn, size[:]); err != nil {
			return
		}
		packet := make([]byte, binary.BigEndian.Uint16(size[:]))
		if _, err := io.ReadFull(conn, packet); err != nil {
			return
		}
		var answer dnsmessage.Message
		if answer.Unpack(packet) != nil {
			return
		}
		answer.Response = true
		answer.Answers = []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: answer.Questions[0].Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 60}, Body: &dnsmessage.AResource{A: [4]byte{93, 184, 216, 34}}}}
		packet, _ = answer.Pack()
		binary.BigEndian.PutUint16(size[:], uint16(len(packet)))
		conn.Write(append(size[:], packet...))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err = probeDNS(ctx, tcp.Addr().String())
	tcp.Close()
	udp.Close()
	<-udpFinished
	<-tcpFinished
	if err != nil {
		t.Fatalf("truncated UDP did not fall back to TCP: %v", err)
	}
}

func TestDNSCancellationClosesStalledSocket(t *testing.T) {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- probeDNS(ctx, conn.LocalAddr().String()) }()
	buffer := make([]byte, 512)
	conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := conn.ReadFrom(buffer); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("canceled query succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("canceled DNS probe remained blocked")
	}
}
