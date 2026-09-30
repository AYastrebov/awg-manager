package subscription

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoaxisr/awg-manager/internal/logging"
)

// captureLog keeps every journal line the service writes.
type captureLog struct {
	mu    sync.Mutex
	lines []string
}

func (c *captureLog) AppLog(_ logging.Level, _, _, _, target, message string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lines = append(c.lines, target+" "+message)
}

func (c *captureLog) all() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.Join(c.lines, "\n")
}

const (
	subURL      = "https://sub.example.com/sub/Tok3nAbc"
	redirectURL = "https://cdn.example.net/r/Hop5ecret?key=K3y"
	shareLink   = "vless://3a3b1c2e-9999-4321-aaaa-1234567890a1@h1.example:443?security=tls#A"
)

var secrets = []string{"Tok3nAbc", "Hop5ecret", "K3y", "3a3b1c2e"}

func assertNoSecret(t *testing.T, where, got string) {
	t.Helper()
	for _, s := range secrets {
		if strings.Contains(got, s) {
			t.Fatalf("%s: %q survived in %q", where, s, got)
		}
	}
}

// TestMaskURL_CatchesWhatExactMatchMisses — MaskURL заменял только точную
// строку настроенного адреса. Ошибка на хопе редиректа цитирует уже новый
// адрес, а ошибка парсера — share-link с uuid сервера. Оба доходят до
// lastError, который показывают REST, веб-интерфейс и журнал.
func TestMaskURL_CatchesWhatExactMatchMisses(t *testing.T) {
	msg := `Get "` + redirectURL + `": x509: unknown authority; line 1 (vless): parse "` + shareLink + `": invalid port; from ` + subURL
	got := MaskURL(msg, subURL)
	assertNoSecret(t, "MaskURL", got)
	if !strings.Contains(got, "<subscription-url>") {
		t.Fatalf("the configured address must still read as <subscription-url>: %q", got)
	}
	if !strings.Contains(got, "cdn.example.net") {
		t.Fatalf("the redirect host tells the reader which hop failed: %q", got)
	}
}

// TestService_JournalCarriesNoURL — сервис пишет причину сбоя в журнал
// (бакет singbox), а get_logs отдаёт его и ключу только для чтения.
// SanitizeLogText там маскирует хост, но не путь и не query.
func TestService_JournalCarriesNoURL(t *testing.T) {
	logs := &captureLog{}
	svc := NewService(nil, nil)
	svc.SetAppLogger(logs)
	svc.logWarn("subscription-refresh", "id1", "fetch failed: Get \""+redirectURL+"\": EOF")
	svc.logInfo("subscription-refresh", "id1", "parser: "+shareLink)
	svc.logDebug("subscription-refresh", "id1", subURL)
	assertNoSecret(t, "journal", logs.all())
	if !strings.Contains(logs.all(), "cdn.example.net") {
		t.Fatalf("the host must stay for diagnosis: %q", logs.all())
	}
}

// TestScheduler_JournalCarriesNoURL — планировщик пишет ошибку refresh как
// есть, а ошибка парсера возвращается немаскированной.
func TestScheduler_JournalCarriesNoURL(t *testing.T) {
	store, _ := NewStore(filepath.Join(t.TempDir(), "s.json"))
	store.Create(CreateInput{Label: "a", URL: "u", RefreshHours: 1, Enabled: true})
	logs := &captureLog{}
	done := make(chan struct{})
	sched := NewScheduler(store, func(context.Context, string) error {
		defer close(done)
		return errors.New(`subscription: ни одной валидной ссылки. Первая ошибка парсера: line 1 (vless): parse "` + shareLink + `": invalid port`)
	})
	sched.SetAppLogger(logs)
	sched.tick(context.Background(), time.Now().Add(2*time.Hour))
	<-done
	// The warning is written after doRefresh returns.
	deadline := time.Now().Add(time.Second)
	for !strings.Contains(logs.all(), "refresh failed") && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(logs.all(), "refresh failed") {
		t.Fatalf("no warning written: %q", logs.all())
	}
	assertNoSecret(t, "scheduler journal", logs.all())
}
