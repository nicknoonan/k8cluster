package wol

import "testing"

func TestBuildMagicPacket(t *testing.T) {
	packet, err := BuildMagicPacket("00:d8:61:59:88:14")
	if err != nil {
		t.Fatalf("BuildMagicPacket returned error: %v", err)
	}
	if got, want := len(packet), 102; got != want {
		t.Fatalf("len(packet) = %d, want %d", got, want)
	}
	for i := 0; i < 6; i++ {
		if packet[i] != 0xff {
			t.Fatalf("packet[%d] = %x, want ff", i, packet[i])
		}
	}
}

func TestBuildMagicPacketRejectsInvalidMAC(t *testing.T) {
	if _, err := BuildMagicPacket("invalid"); err == nil {
		t.Fatal("expected error for invalid mac")
	}
}
