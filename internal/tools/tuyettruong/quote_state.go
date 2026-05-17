package tuyettruong

import (
	"sync"
	"time"
)

// Draft is the in-progress quote the sales agent is building. Lives in process
// memory keyed by goclaw session_key (which is per-customer-per-channel).
// Lost on goclaw restart — acceptable for v1; customer just re-adds items.
// When durability becomes a need, swap to session.Metadata JSON-encoded.

type DraftItem struct {
	ProductSlug       string            `json:"productSlug"`
	ProductName       string            `json:"productName"`
	VariantSku        string            `json:"variantSku"`
	VariantAttributes map[string]string `json:"variantAttributes"`
	UnitPriceSnapshot float64           `json:"unitPriceSnapshot"`
	Qty               int               `json:"qty"`
	ImageURL          string            `json:"imageUrl,omitempty"`
}

type DraftCustomer struct {
	Name    string `json:"name,omitempty"`
	Phone   string `json:"phone,omitempty"`
	Email   string `json:"email,omitempty"`
	Address string `json:"address,omitempty"`
	Note    string `json:"note,omitempty"`
}

type Draft struct {
	Items     []DraftItem   `json:"items"`
	Customer  DraftCustomer `json:"customer"`
	CreatedAt time.Time     `json:"createdAt"`
	UpdatedAt time.Time     `json:"updatedAt"`
}

// Subtotal sums line totals from item snapshots. Never trust this for ordering
// — always re-fetch prices in quote_finalize before order_place.
func (d *Draft) Subtotal() float64 {
	var s float64
	for _, it := range d.Items {
		s += it.UnitPriceSnapshot * float64(it.Qty)
	}
	return s
}

func (d *Draft) findIndex(sku string) int {
	for i, it := range d.Items {
		if it.VariantSku == sku {
			return i
		}
	}
	return -1
}

// store holds drafts keyed by goclaw session key. One global instance per
// process. Expired drafts (>24h) are pruned lazily on access.
type draftStore struct {
	mu     sync.RWMutex
	drafts map[string]*Draft
}

var drafts = &draftStore{drafts: make(map[string]*Draft)}

const draftTTL = 24 * time.Hour

// loadOrInit returns the draft for a session, creating a fresh one if missing
// or expired. Always non-nil.
func (s *draftStore) loadOrInit(sessionKey string) *Draft {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.drafts[sessionKey]
	if !ok || now.Sub(d.CreatedAt) > draftTTL {
		d = &Draft{Items: []DraftItem{}, CreatedAt: now, UpdatedAt: now}
		s.drafts[sessionKey] = d
	}
	return d
}

func (s *draftStore) load(sessionKey string) *Draft {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.drafts[sessionKey]
	if !ok {
		return nil
	}
	if time.Since(d.CreatedAt) > draftTTL {
		return nil
	}
	return d
}

func (s *draftStore) clear(sessionKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.drafts, sessionKey)
}
