package pg

import (
	"database/sql"
	"log/slog"
	"sync"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/cron"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const defaultCronCacheTTL = 2 * time.Minute

// PGCronStore implements store.CronStore backed by Postgres.
// GetDueJobs() uses an in-memory cache with TTL to reduce DB polling (1s interval).
type PGCronStore struct {
	db      *sql.DB
	mu      sync.Mutex
	onJob   func(job *store.CronJob) (*store.CronJobResult, error)
	onEvent func(event store.CronEvent)
	running bool
	stop    chan struct{}

	// Job cache: reduces GetDueJobs polling from 86,400 queries/day to ~720/day
	jobCache    []store.CronJob
	cacheLoaded bool
	cacheTime   time.Time
	cacheTTL    time.Duration

	retryCfg  cron.RetryConfig
	defaultTZ string // fallback IANA timezone for cron jobs without explicit TZ
}

func NewPGCronStore(db *sql.DB) *PGCronStore {
	return &PGCronStore{db: db, cacheTTL: defaultCronCacheTTL, retryCfg: cron.DefaultRetryConfig()}
}

// SetRetryConfig overrides the default retry configuration.
func (s *PGCronStore) SetRetryConfig(cfg cron.RetryConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.retryCfg = cfg
}

// SetDefaultTimezone sets the fallback IANA timezone for cron expressions
// when a job does not specify its own timezone.
func (s *PGCronStore) SetDefaultTimezone(tz string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.defaultTZ = tz
}

func (s *PGCronStore) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return nil
	}
	s.stop = make(chan struct{})
	s.running = true
	s.recomputeStaleJobs()
	go s.runLoop()
	slog.Info("pg cron service started")
	return nil
}

func (s *PGCronStore) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	close(s.stop)
	s.running = false
}

func (s *PGCronStore) SetOnJob(handler func(job *store.CronJob) (*store.CronJobResult, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onJob = handler
}

func (s *PGCronStore) SetOnEvent(handler func(event store.CronEvent)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onEvent = handler
}

func (s *PGCronStore) emitEvent(event store.CronEvent) {
	s.mu.Lock()
	fn := s.onEvent
	s.mu.Unlock()
	if fn != nil {
		fn(event)
	}
}
