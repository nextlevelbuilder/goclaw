package browser

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// maskedValue replaces any field value that may hold a secret in a snapshot.
const maskedValue = "[REDACTED]"

// secretSession records that a vault secret was typed into a tab. While it is
// active the tab belongs to one (scope, run session) pair and every channel
// that could read the page back to the model is closed or masked.
type secretSession struct {
	owner     string // scope key + run session key that filled the secret
	origin    string // scheme://host[:port] of the top frame at fill time
	fillURL   string // full top-frame URL at fill time
	submitted bool   // the fill pressed Enter; the next navigation ends the session
	maskAll   bool   // take over: every input value is hidden, not just the filled fields
	mu        sync.Mutex
	masked    map[int]bool // backend DOM node ids of fields that received a secret
}

func (s *secretSession) maskedIDs() map[int]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[int]bool, len(s.masked))
	for id := range s.masked {
		out[id] = true
	}
	return out
}

func (s *secretSession) addMasked(id int) {
	if id == 0 {
		return
	}
	s.mu.Lock()
	s.masked[id] = true
	s.mu.Unlock()
}

// secretOwnerKey identifies who may touch a secret-bearing tab.
func secretOwnerKey(scope BrowserScope, runSession string) string {
	return scope.Key() + "|session=" + runSession
}

// topOrigin splits a page URL into its origin and hostname.
func topOrigin(raw string) (origin, host string, err error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", "", fmt.Errorf("page has no web origin")
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host), strings.ToLower(u.Hostname()), nil
}

// checkFillOrigin accepts https pages, and plain http only for loopback hosts
// (which the secret's own allowed-domain list must still name).
func checkFillOrigin(raw string) (host string, err error) {
	origin, host, err := topOrigin(raw)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(origin, "https://") {
		return host, nil
	}
	if strings.HasPrefix(origin, "http://") && (host == "localhost" || host == "127.0.0.1" || host == "::1") {
		return host, nil
	}
	return "", fmt.Errorf("secrets can only be filled on https pages")
}

// secretBlockedOps are page-reading operations closed while a secret is on the page.
var secretBlockedOps = map[string]bool{"evaluate": true, "wait_fn": true, "console": true, "screenshot": true}

// enterSecretSession registers (or extends) the secret session of a tab.
func (m *Manager) enterSecretSession(targetID, owner, pageURL string, submitted bool, maskedNode int) {
	origin, _, err := topOrigin(pageURL)
	if err != nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.secretTabs == nil {
		m.secretTabs = make(map[string]*secretSession)
	}
	s := m.secretTabs[targetID]
	if s == nil || s.origin != origin {
		s = &secretSession{owner: owner, origin: origin, masked: map[int]bool{}}
		m.secretTabs[targetID] = s
	}
	s.fillURL = pageURL
	s.submitted = s.submitted || submitted
	s.addMasked(maskedNode)
}

// syncSecretURL ends the session when the tab left the origin it was filled on,
// or when a submitted form navigated. It returns the session still in force.
func (m *Manager) syncSecretURL(targetID, currentURL string) *secretSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.secretTabs[targetID]
	if s == nil {
		return nil
	}
	origin, _, err := topOrigin(currentURL)
	if err != nil || origin != s.origin || (s.submitted && currentURL != s.fillURL) {
		delete(m.secretTabs, targetID)
		return nil
	}
	return s
}

// guardSecretTab enforces tab binding and the blocked operations for a tab that
// holds a secret. op is an action name ("snapshot", "click", ...) or one of
// secretBlockedOps. It is a no-op for tabs without a secret session.
func (m *Manager) guardSecretTab(ctx context.Context, targetID, op, runSession string) error {
	m.mu.Lock()
	if len(m.secretTabs) == 0 {
		m.mu.Unlock()
		return nil
	}
	tid := targetID
	if tid == "" {
		if page, err := m.getPageForTenant("", tenantIDFromCtx(ctx)); err == nil && page != nil {
			tid = string(page.TargetID)
		}
	}
	s := m.secretTabs[tid]
	page := m.pages[tid]
	m.mu.Unlock()
	if s == nil {
		return nil
	}
	if s.owner != secretOwnerKey(scopeFromCtx(ctx), runSession) {
		return fmt.Errorf("tab not found: %s", targetID)
	}
	if page != nil {
		if info, err := page.Info(); err == nil && info != nil {
			s = m.syncSecretURL(tid, info.URL)
		}
	}
	if s != nil && secretBlockedOps[op] {
		return fmt.Errorf("%s is disabled while a secret is on the page; use snapshot and act instead", strings.TrimSuffix(op, "_fn"))
	}
	return nil
}

// secretMaskIDs lists the DOM nodes whose values a snapshot must hide: fields
// that received a secret plus every password input on the page. Nil when the
// tab holds no secret.
func (m *Manager) secretMaskIDs(page *rod.Page, targetID string) map[int]bool {
	m.mu.Lock()
	s := m.secretTabs[targetID]
	m.mu.Unlock()
	if s == nil {
		return nil
	}
	ids := s.maskedIDs()
	if s.maskAll {
		if els, err := page.Elements("input, textarea"); err == nil {
			for _, el := range els {
				if n, err := (proto.DOMDescribeNode{ObjectID: el.Object.ObjectID}).Call(page); err == nil && n.Node != nil {
					ids[int(n.Node.BackendNodeID)] = true
				}
			}
		}
	}
	if els, err := page.Elements("input[type=password]"); err == nil {
		for _, el := range els {
			if n, err := (proto.DOMDescribeNode{ObjectID: el.Object.ObjectID}).Call(page); err == nil && n.Node != nil {
				ids[int(n.Node.BackendNodeID)] = true
			}
		}
	}
	return ids
}
