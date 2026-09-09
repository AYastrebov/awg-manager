package managed

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/hoaxisr/awg-manager/internal/storage"
)

// TestNextFreePeerIP — адрес выдаётся вместо пользователя, поэтому ошибка
// здесь тихая: пир создастся и просто не будет работать. Повторяет
// suggestNextPeerIP из веб-интерфейса.
func TestNextFreePeerIP(t *testing.T) {
	cases := []struct {
		name    string
		address string
		used    []string
		want    string
	}{
		{"first peer starts at .2", "10.0.0.1/24", nil, "10.0.0.2/32"},
		{"skips taken addresses", "10.0.0.1/24", []string{"10.0.0.2/32", "10.0.0.3/32"}, "10.0.0.4/32"},
		{"fills a gap left by a deleted peer", "10.0.0.1/24", []string{"10.0.0.3/32", "10.0.0.4/32"}, "10.0.0.2/32"},
		// The server's own address must never be handed to a client, even
		// when it sits in the middle of the range.
		{"never hands out the server address", "10.0.0.2/24", []string{}, "10.0.0.3/32"},
		{"address without a prefix works", "192.168.9.1", []string{"192.168.9.2"}, "192.168.9.3/32"},
		{"used entries without a prefix still count", "10.0.0.1/24", []string{"10.0.0.2"}, "10.0.0.3/32"},
		{"ignores entries that are not addresses", "10.0.0.1/24", []string{"", "garbage", "10.0.0.2/32"}, "10.0.0.3/32"},
		// No address is better than a wrong one: the caller must ask.
		{"refuses when the server address is unusable", "not-an-ip", nil, ""},
		{"refuses an IPv6 server address", "fd00::1/64", nil, ""},
	}
	for _, c := range cases {
		if got := NextFreePeerIP(c.address, c.used); got != c.want {
			t.Errorf("%s: NextFreePeerIP(%q, %v) = %q, want %q", c.name, c.address, c.used, got, c.want)
		}
	}
}

// TestNextFreePeerIPExhausted — когда свободных нет, пустая строка
// заставляет вызывающего сказать об этом, а не выдать .255 или .0.
func TestNextFreePeerIPExhausted(t *testing.T) {
	used := make([]string, 0, 253)
	for n := 2; n < 255; n++ {
		used = append(used, fmt.Sprintf("10.0.0.%d/32", n))
	}
	if got := NextFreePeerIP("10.0.0.1/24", used); got != "" {
		t.Fatalf("a full subnet returned %q, want an empty result", got)
	}
}

// TestAddPeer_AllocatesTunnelIPWhenEmpty — пустой TunnelIP означает «выдать
// первый свободный адрес в подсети сервера». Раньше это делал адаптер MCP
// своей копией правила; теперь правило одно и лежит рядом с AddPeer.
func TestAddPeer_AllocatesTunnelIPWhenEmpty(t *testing.T) {
	svc, store, _ := newCreateTestService(t)
	if err := store.AddManagedServer(storage.ManagedServer{
		InterfaceName: "Wireguard1", Address: "10.0.0.1", Mask: "255.255.255.0", ListenPort: 51820, Policy: "none",
		Peers: []storage.ManagedPeer{{PublicKey: testPeerPubKey, TunnelIP: "10.0.0.2/32", Description: "laptop", Enabled: true}},
	}); err != nil {
		t.Fatal(err)
	}
	peer, err := svc.AddPeer(context.Background(), "Wireguard1", AddPeerRequest{Description: "phone"})
	if err != nil {
		t.Fatal(err)
	}
	if peer.TunnelIP != "10.0.0.3/32" {
		t.Fatalf("allocated %q, want the first free address after the existing peer", peer.TunnelIP)
	}
	stored, _ := store.GetManagedServerByID("Wireguard1")
	if len(stored.Peers) != 2 || stored.Peers[1].TunnelIP != "10.0.0.3/32" {
		t.Fatalf("persisted peers = %+v", stored.Peers)
	}

	// An explicit address is still validated, not replaced.
	if _, err := svc.AddPeer(context.Background(), "Wireguard1", AddPeerRequest{Description: "x", TunnelIP: "not-a-cidr"}); err == nil {
		t.Fatal("an invalid explicit address must still be refused")
	}

	// A server whose address the allocator cannot use yields a typed error
	// the caller can turn into "ask the user", not a peer with no address.
	if err := store.AddManagedServer(storage.ManagedServer{InterfaceName: "Wireguard6", Address: "fd00::1", Mask: "255.255.255.0", ListenPort: 51821, Policy: "none"}); err != nil {
		t.Fatal(err)
	}
	_, err = svc.AddPeer(context.Background(), "Wireguard6", AddPeerRequest{Description: "phone"})
	if !errors.Is(err, ErrNoFreePeerIP) {
		t.Fatalf("err = %v, want ErrNoFreePeerIP", err)
	}
}
