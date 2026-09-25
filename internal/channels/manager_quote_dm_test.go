package channels

import "testing"

type quoteDMChannel struct {
	Channel
	optIn bool
}

func (q *quoteDMChannel) QuoteInboundOnDM() bool { return q.optIn }

func TestManager_QuoteInboundOnDM(t *testing.T) {
	m := NewManager(nil)

	if m.QuoteInboundOnDM("missing") {
		t.Error("missing channel must be false")
	}

	plain := newMockChannel("plain", TypeZaloOA)
	m.RegisterChannel("plain", plain)
	if m.QuoteInboundOnDM("plain") {
		t.Error("non-DMQuoteChannel must be false")
	}

	off := &quoteDMChannel{Channel: newMockChannel("off", TypeZaloOA), optIn: false}
	m.RegisterChannel("off", off)
	if m.QuoteInboundOnDM("off") {
		t.Error("opt-in false must be false")
	}

	on := &quoteDMChannel{Channel: newMockChannel("on", TypeZaloOA), optIn: true}
	m.RegisterChannel("on", on)
	if !m.QuoteInboundOnDM("on") {
		t.Error("opt-in true must be true")
	}
}
