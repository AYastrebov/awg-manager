package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hoaxisr/awg-manager/internal/ndms"
)

type fakeHotspot struct {
	devices []ndms.Device
	err     error
}

func (f *fakeHotspot) List(ctx context.Context) ([]ndms.Device, error) {
	return f.devices, f.err
}

func TestSingboxConnections_Clients_HappyPath(t *testing.T) {
	hot := &fakeHotspot{devices: []ndms.Device{
		{IP: "192.168.1.5", Name: "iPhone"},
		{IP: "192.168.1.7", Name: "macbook"},
		{IP: "192.168.1.9", Hostname: "android-tablet"},
	}}
	h := NewSingboxConnectionsHandler(hot)

	req := httptest.NewRequest(http.MethodGet, "/api/singbox/connections/clients", nil)
	rec := httptest.NewRecorder()
	h.Clients(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}
	var resp SingboxConnectionsClientsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Success {
		t.Fatalf("success=false")
	}
	want := map[string]string{
		"192.168.1.5": "iPhone",
		"192.168.1.7": "macbook",
		"192.168.1.9": "android-tablet",
	}
	if len(resp.Data.ClientsByIP) != len(want) {
		t.Fatalf("map size: got %d, want %d", len(resp.Data.ClientsByIP), len(want))
	}
	for k, v := range want {
		if got := resp.Data.ClientsByIP[k]; got != v {
			t.Errorf("key %s: got %q, want %q", k, got, v)
		}
	}
}

func TestSingboxConnections_Clients_EmptyHotspot(t *testing.T) {
	h := NewSingboxConnectionsHandler(&fakeHotspot{devices: nil})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.Clients(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", rec.Code)
	}
	var resp SingboxConnectionsClientsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Data.ClientsByIP == nil {
		t.Fatal("clientsByIP should be non-nil empty map, not nil")
	}
	if len(resp.Data.ClientsByIP) != 0 {
		t.Fatalf("expected empty map, got %v", resp.Data.ClientsByIP)
	}
}

func TestSingboxConnections_Clients_HotspotError(t *testing.T) {
	h := NewSingboxConnectionsHandler(&fakeHotspot{err: errors.New("ndms boom")})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.Clients(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200 (best-effort)", rec.Code)
	}
	var resp SingboxConnectionsClientsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Data.ClientsByIP) != 0 {
		t.Fatalf("expected empty map on error, got %v", resp.Data.ClientsByIP)
	}
}

func TestSingboxConnections_Clients_NilHotspot(t *testing.T) {
	h := NewSingboxConnectionsHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.Clients(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d, want 503", rec.Code)
	}
}

func TestSingboxConnections_Clients_NotGet(t *testing.T) {
	h := NewSingboxConnectionsHandler(&fakeHotspot{})
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	h.Clients(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status: got %d, want 405", rec.Code)
	}
}

func TestSingboxConnections_Clients_LowercaseIPKeys(t *testing.T) {
	hot := &fakeHotspot{devices: []ndms.Device{
		{IP: "FE80::1234", Name: "ipv6-host"},
	}}
	h := NewSingboxConnectionsHandler(hot)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.Clients(rec, req)
	var resp SingboxConnectionsClientsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if got := resp.Data.ClientsByIP["fe80::1234"]; got != "ipv6-host" {
		t.Fatalf("expected lowercase key match, got map=%v", resp.Data.ClientsByIP)
	}
}

func TestSingboxConnections_Clients_PrefersNameOverHostname(t *testing.T) {
	hot := &fakeHotspot{devices: []ndms.Device{
		{IP: "192.168.1.5", Name: "iPhone", Hostname: "anya-iphone"},
	}}
	h := NewSingboxConnectionsHandler(hot)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.Clients(rec, req)
	var resp SingboxConnectionsClientsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if got := resp.Data.ClientsByIP["192.168.1.5"]; got != "iPhone" {
		t.Fatalf("expected Name to win, got %q", got)
	}
}

func TestSingboxConnections_Clients_SkipsEmptyName(t *testing.T) {
	hot := &fakeHotspot{devices: []ndms.Device{
		{IP: "192.168.1.5"},
		{IP: "192.168.1.6", Name: "named"},
	}}
	h := NewSingboxConnectionsHandler(hot)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.Clients(rec, req)
	var resp SingboxConnectionsClientsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if _, present := resp.Data.ClientsByIP["192.168.1.5"]; present {
		t.Fatalf("expected nameless device to be skipped, got %v", resp.Data.ClientsByIP)
	}
	if resp.Data.ClientsByIP["192.168.1.6"] != "named" {
		t.Fatal("named device missing")
	}
}
