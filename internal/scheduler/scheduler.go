package scheduler

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/IS908/optix/internal/datastore/sqlite"
)

// Config holds scheduler configuration.
type Config struct {
	WorkerCount    int           // Number of worker goroutines (default: 5)
	QueueSize      int           // Task queue buffer size (default: 100)
	TickInterval   time.Duration // How often to check for new tasks (default: 1 minute)
	WorkerThrottle time.Duration // Delay between tasks per worker (default: 12 seconds)
}

// Scheduler orchestrates background refresh tasks.
type Scheduler struct {
	cfg         Config
	store       *sqlite.Store
	ibCfg       IBConfig
	analysisCfg AnalysisConfig

	taskQueue chan Task
	workers   []*Worker
	ticker    *time.Ticker

	mu        sync.Mutex
	lastBatch map[int]time.Time // interval → last batch dispatch time
}

// New creates a new Scheduler instance.
func New(cfg Config, store *sqlite.Store, ibCfg IBConfig, analysisCfg AnalysisConfig) *Scheduler {
	// Set defaults
	if cfg.WorkerCount == 0 {
		cfg.WorkerCount = 5
	}
	if cfg.QueueSize == 0 {
		cfg.QueueSize = 100
	}
	if cfg.TickInterval == 0 {
		cfg.TickInterval = 1 * time.Minute
	}
	if cfg.WorkerThrottle == 0 {
		cfg.WorkerThrottle = 12 * time.Second
	}

	return &Scheduler{
		cfg:         cfg,
		store:       store,
		ibCfg:       ibCfg,
		analysisCfg: analysisCfg,
		taskQueue:   make(chan Task, cfg.QueueSize),
		workers:     make([]*Worker, 0, cfg.WorkerCount),
		lastBatch:   make(map[int]time.Time),
	}
}

// Start initializes workers and begins task generation.
func (s *Scheduler) Start(ctx context.Context) error {
	// Initialize workers (pass taskQueue for both read and retry write)
	for i := 0; i < s.cfg.WorkerCount; i++ {
		w := NewWorker(i, s.taskQueue, s.taskQueue, s.store, s.ibCfg, s.analysisCfg, s.cfg.WorkerThrottle)
		s.workers = append(s.workers, w)
		go w.Run(ctx)
	}

	log.Info().
		Int("workers", s.cfg.WorkerCount).
		Dur("tick_interval", s.cfg.TickInterval).
		Msg("Scheduler initialized")

	// Start task generator
	s.ticker = time.NewTicker(s.cfg.TickInterval)
	go s.generateTasks(ctx)

	return nil
}

// generateTasks runs the task generation loop.
func (s *Scheduler) generateTasks(ctx context.Context) {
	// Periodic prune: clean stale data once per hour
	pruneTicker := time.NewTicker(1 * time.Hour)
	defer pruneTicker.Stop()

	for {
		select {
		case <-s.ticker.C:
			s.generateBatch()
		case <-pruneTicker.C:
			if n, err := s.store.PruneStaleData(ctx); err != nil {
				log.Error().Err(err).Msg("Failed to prune stale data")
			} else if n > 0 {
				log.Info().Int64("deleted_rows", n).Msg("Pruned stale data")
			}
		case <-ctx.Done():
			s.ticker.Stop()
			return
		}
	}
}

// generateBatch queries symbols needing refresh and dispatches tasks.
// Implements hybrid batching: small batches (3-5 symbols) distributed over time.
func (s *Scheduler) generateBatch() {
	// Query symbols needing refresh
	symbols, err := s.store.GetSymbolsNeedingRefresh()
	if err != nil {
		log.Error().Err(err).Msg("Failed to query symbols needing refresh")
		return
	}

	if len(symbols) == 0 {
		return // No work to do
	}

	// Group by refresh interval
	grouped := groupByInterval(symbols)

	now := time.Now()

	// For each interval group, dispatch small batches
	for interval, batch := range grouped {
		s.mu.Lock()
		lastTime := s.lastBatch[interval]
		s.mu.Unlock()

		// Check if it's time to dispatch a batch for this interval
		if !shouldDispatchBatch(interval, lastTime, now, len(batch)) {
			continue
		}

		// Take up to 5 symbols for this batch
		batchSize := min(5, len(batch))
		tasks := batch[:batchSize]

		log.Debug().
			Int("interval", interval).
			Int("batch_size", batchSize).
			Int("total_symbols", len(batch)).
			Msg("Dispatching batch")

		for _, symbol := range tasks {
			select {
			case s.taskQueue <- Task{
				Symbol:    symbol,
				Type:      "analyze",
				CreatedAt: now,
			}:
				// Update last refresh time immediately to prevent re-queuing
				if err := s.store.UpdateLastRefreshTime(symbol, now); err != nil {
					log.Error().Err(err).Str("symbol", symbol).Msg("Failed to update last refresh time")
				}

			default:
				log.Warn().
					Str("symbol", symbol).
					Msg("Task queue full, dropping task")
			}
		}

		// Update last batch time for this interval
		s.mu.Lock()
		s.lastBatch[interval] = now
		s.mu.Unlock()
	}
}
