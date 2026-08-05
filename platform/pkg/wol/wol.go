package wol

import (
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
)

type Sender interface {
	Send(ctx context.Context, macAddress string) error
}

type DefaultSender struct{}

func (DefaultSender) Send(ctx context.Context, macAddress string) error {
	packet, err := BuildMagicPacket(macAddress)
	if err != nil {
		return err
	}

	conn, err := net.Dial("udp4", "255.255.255.255:9")
	if err != nil {
		return fmt.Errorf("dial udp: %w", err)
	}
	defer conn.Close()

	if _, err := conn.Write(packet); err != nil {
		return fmt.Errorf("send wake-on-lan packet: %w", err)
	}
	return nil
}

func BuildMagicPacket(macAddress string) ([]byte, error) {
	cleaned := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(macAddress), ":", ""), "-", "")
	if len(cleaned) != 12 {
		return nil, fmt.Errorf("invalid mac address: %q", macAddress)
	}

	macBytes, err := hex.DecodeString(cleaned)
	if err != nil {
		return nil, fmt.Errorf("decode mac address: %w", err)
	}

	packet := make([]byte, 6+16*len(macBytes))
	for i := 0; i < 6; i++ {
		packet[i] = 0xff
	}
	for i := 6; i < len(packet); i += len(macBytes) {
		copy(packet[i:], macBytes)
	}
	return packet, nil
}
