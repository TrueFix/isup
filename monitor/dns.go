package monitor

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"

	"golang.org/x/net/dns/dnsmessage"
)

// Query the literal resolver directly. net.Resolver.LookupIP would also consult
// the hosts file, which could falsely look like a successful network probe.
func probeDNS(ctx context.Context, address string) error {
	var idBytes [2]byte
	if _, err := rand.Read(idBytes[:]); err != nil {
		return err
	}
	id := binary.BigEndian.Uint16(idBytes[:])
	question := dnsmessage.Question{Name: dnsmessage.MustNewName("example.com."), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}
	query := dnsmessage.Message{Header: dnsmessage.Header{ID: id, RecursionDesired: true}, Questions: []dnsmessage.Question{question}}
	packet, err := query.Pack()
	if err != nil {
		return err
	}
	answer, err := exchangeDNS(ctx, "udp", address, packet)
	if err != nil {
		return err
	}
	if answer.Header.Truncated {
		answer, err = exchangeDNS(ctx, "tcp", address, packet)
		if err != nil {
			return err
		}
	}
	if answer.ID != id || !answer.Response || answer.OpCode != 0 || answer.Truncated || answer.RCode != dnsmessage.RCodeSuccess || len(answer.Questions) != 1 {
		return errors.New("invalid or unsuccessful DNS response")
	}
	q := answer.Questions[0]
	if !strings.EqualFold(q.Name.String(), question.Name.String()) || q.Type != question.Type || q.Class != question.Class {
		return errors.New("DNS response did not match the query")
	}
	for _, resource := range answer.Answers {
		if record, ok := resource.Body.(*dnsmessage.AResource); ok {
			ip := net.IP(record.A[:])
			if ip.IsGlobalUnicast() && !ip.IsPrivate() {
				return nil
			}
		}
	}
	return errors.New("DNS did not return a public IPv4 address for example.com")
}

func exchangeDNS(ctx context.Context, network, address string, packet []byte) (dnsmessage.Message, error) {
	var response dnsmessage.Message
	conn, err := (&net.Dialer{}).DialContext(ctx, network, address)
	if err != nil {
		return response, err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return response, err
		}
	}
	if network == "tcp" {
		framed := make([]byte, 2+len(packet))
		binary.BigEndian.PutUint16(framed, uint16(len(packet)))
		copy(framed[2:], packet)
		_, err = io.Copy(conn, bytes.NewReader(framed))
	} else {
		_, err = conn.Write(packet)
	}
	if err != nil {
		return response, err
	}
	var data []byte
	if network == "tcp" {
		var size [2]byte
		if _, err := io.ReadFull(conn, size[:]); err != nil {
			return response, err
		}
		data = make([]byte, int(binary.BigEndian.Uint16(size[:])))
		if _, err := io.ReadFull(conn, data); err != nil {
			return response, err
		}
	} else {
		data = make([]byte, 4096)
		n, err := conn.Read(data)
		if err != nil {
			return response, err
		}
		data = data[:n]
	}
	if network == "udp" {
		var parser dnsmessage.Parser
		header, err := parser.Start(data)
		if err != nil {
			return response, fmt.Errorf("malformed DNS header: %w", err)
		}
		if header.Truncated {
			// A truncated packet may end in the middle of a record, so its
			// complete message need not be parseable before TCP fallback.
			response.Header = header
			return response, nil
		}
	}
	if err := response.Unpack(data); err != nil {
		return response, fmt.Errorf("malformed DNS response: %w", err)
	}
	return response, nil
}
