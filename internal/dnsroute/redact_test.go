package dnsroute

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/hoaxisr/awg-manager/internal/logging"
)

type failingDownloader struct{ err error }

func (d failingDownloader) ReadAll(context.Context, SubscriptionDownloadRequest) ([]byte, SubscriptionDownloadMeta, error) {
	return nil, SubscriptionDownloadMeta{}, d.err
}

type captureAppLog struct {
	mu    sync.Mutex
	lines []string
}

func (c *captureAppLog) AppLog(_ logging.Level, _, _, _, target, message string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lines = append(c.lines, target+" "+message)
}

// TestRefreshSubscriptions_KeepsTheTokenOut — загрузчик net/http цитирует
// адрес целиком, в том числе адрес хопа редиректа. Этот текст ложился в
// lastError (его показывают REST и MCP get_dns_route, хотя само поле url
// там уже очищено от query) и строкой url=… в журнал.
func TestRefreshSubscriptions_KeepsTheTokenOut(t *testing.T) {
	// A public IP literal: the early guard resolves a name, and a test must
	// not depend on DNS.
	const listURL = "https://1.1.1.1/l/Tok3nAbc?key=K3y"
	svc := newTestService(t)
	logs := &captureAppLog{}
	svc.appLog = logging.NewScopedLogger(logs, logging.GroupRouting, logging.SubDnsRoute)
	svc.SetDownloader(failingDownloader{err: errors.New(`Get "https://cdn.example.net/r/Hop5ecret": dial tcp: i/o timeout`)})

	data := svc.store.GetCached()
	data.Lists = append(data.Lists, DomainList{ID: "l1", Name: "geo", Subscriptions: []Subscription{{URL: listURL}}})
	if err := svc.store.Save(data); err != nil {
		t.Fatal(err)
	}
	_ = svc.refreshSubscriptions(context.Background(), "l1")

	got := svc.store.GetCached().Lists[0].Subscriptions[0]
	if got.LastError == "" {
		t.Fatal("a failed fetch must still be recorded")
	}
	if !strings.Contains(got.LastError, "cdn.example.net") {
		t.Fatalf("the host tells the reader which hop failed: %q", got.LastError)
	}
	journal := strings.Join(logs.lines, "\n")
	for where, text := range map[string]string{"lastError": got.LastError, "journal": journal} {
		for _, secret := range []string{"Tok3nAbc", "K3y", "Hop5ecret"} {
			if strings.Contains(text, secret) {
				t.Fatalf("%s: %q survived in %q", where, secret, text)
			}
		}
	}
}
