package replycontext

import (
	"strings"
	"sync"
	"time"
)

const (
	defaultMaxDepth           = 8
	defaultMaxTotalChars      = 6000
	defaultMaxCharsPerMessage = 1000
	defaultMaxEntries         = 5000
	defaultTTL                = 24 * time.Hour
)

// Scope isolates reply context by tenant, channel instance, and conversation.
type Scope struct {
	TenantID  string
	ChannelID string
	ThreadID  string
}

// Message is a platform-normalized accepted message stored for future replies.
type Message struct {
	IDs       []string
	ParentIDs []string
	Sender    string
	Body      string
	CreatedAt time.Time
}

// Quote is the platform-normalized immediate quote payload on a reply.
type Quote struct {
	IDs    []string
	Sender string
	Body   string
}

type Options struct {
	MaxDepth           int
	MaxTotalChars      int
	MaxCharsPerMessage int
	MaxEntries         int
	TTL                time.Duration
}

type Cache struct {
	mu      sync.Mutex
	nodes   map[cacheKey]*node
	aliases map[cacheKey]cacheKey
	opts    Options
	now     func() time.Time
}

type cacheKey struct {
	tenant  string
	channel string
	thread  string
	message string
}

type node struct {
	key       cacheKey
	ids       []string
	parentIDs []string
	sender    string
	body      string
	createdAt time.Time
}

func DefaultOptions() Options {
	return Options{
		MaxDepth:           defaultMaxDepth,
		MaxTotalChars:      defaultMaxTotalChars,
		MaxCharsPerMessage: defaultMaxCharsPerMessage,
		MaxEntries:         defaultMaxEntries,
		TTL:                defaultTTL,
	}
}

func NewCache(opts Options) *Cache {
	opts = opts.withDefaults()
	return &Cache{
		nodes:   make(map[cacheKey]*node),
		aliases: make(map[cacheKey]cacheKey),
		opts:    opts,
		now:     time.Now,
	}
}

func (o Options) withDefaults() Options {
	def := DefaultOptions()
	if o.MaxDepth <= 0 {
		o.MaxDepth = def.MaxDepth
	}
	if o.MaxTotalChars <= 0 {
		o.MaxTotalChars = def.MaxTotalChars
	}
	if o.MaxCharsPerMessage <= 0 {
		o.MaxCharsPerMessage = def.MaxCharsPerMessage
	}
	if o.MaxEntries <= 0 {
		o.MaxEntries = def.MaxEntries
	}
	if o.TTL <= 0 {
		o.TTL = def.TTL
	}
	return o
}

func (c *Cache) Store(scope Scope, msg Message) {
	if c == nil {
		return
	}
	ids := NormalizeIDs(msg.IDs)
	body := truncateText(strings.TrimSpace(msg.Body), c.opts.MaxCharsPerMessage)
	if len(ids) == 0 || body == "" {
		return
	}
	now := c.now()
	createdAt := msg.CreatedAt
	if createdAt.IsZero() {
		createdAt = now
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.purgeExpiredLocked(now)
	key := scopedKey(scope, ids[0])
	if old := c.nodes[key]; old != nil {
		c.deleteNodeLocked(key, old)
	}
	n := &node{
		key:       key,
		ids:       ids,
		parentIDs: NormalizeIDs(msg.ParentIDs),
		sender:    strings.TrimSpace(msg.Sender),
		body:      body,
		createdAt: createdAt,
	}
	c.nodes[key] = n
	for _, id := range ids {
		c.aliases[scopedKey(scope, id)] = key
	}
	c.evictLocked()
}

func (c *Cache) Build(scope Scope, quote Quote) string {
	if c == nil {
		return RenderQuote(quote)
	}
	quote.IDs = NormalizeIDs(quote.IDs)

	c.mu.Lock()
	defer c.mu.Unlock()

	c.purgeExpiredLocked(c.now())
	if n := c.lookupLocked(scope, quote.IDs); n != nil {
		if chain := c.collectChainLocked(scope, n); len(chain) > 0 {
			if rendered := renderChain(chain, c.opts); rendered != "" {
				return rendered
			}
		}
	}
	return renderQuote(quote, c.opts)
}

func (c *Cache) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.nodes)
	clear(c.aliases)
}
