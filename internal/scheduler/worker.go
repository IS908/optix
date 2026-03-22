package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	analysisv1 "github.com/IS908/optix/gen/go/optix/analysis/v1"
	"github.com/IS908/optix/internal/analysis"
	"github.com/IS908/optix/internal/broker"
	"github.com/IS908/optix/internal/broker/ibkr"
	"github.com/IS908/optix/internal/datastore/sqlite"
	"github.com/IS908/optix/internal/server"
	"github.com/IS908/optix/internal/webui"
	"github.com/IS908/optix/pkg/model"
)

// Worker processes background refresh tasks from the queue.
type Worker struct {
	id          int
	queue       <-chan Task
	retryQueue  chan<- Task // Separate channel for retry submissions
	store       *sqlite.Store
	ibCfg       IBConfig
	analysisCfg AnalysisConfig
	throttle    time.Duration
}

// IBConfig holds IBKR connection parameters.
type IBConfig struct {
	Host      string
	Port      int
	PythonBin string // Python interpreter for yfinance fallback
}

// AnalysisConfig holds Python analysis engine parameters.
type AnalysisConfig struct {
	Addr          string
	Capital       float64
	ForecastDays  int32
	RiskTolerance string
}

// NewWorker creates a new background worker.
func NewWorker(id int, queue <-chan Task, retryQueue chan<- Task, store *sqlite.Store, ibCfg IBConfig, analysisCfg AnalysisConfig, throttle time.Duration) *Worker {
	return &Worker{
		id:          id,
		queue:       queue,
		retryQueue:  retryQueue,
		store:       store,
		ibCfg:       ibCfg,
		analysisCfg: analysisCfg,
		throttle:    throttle,
	}
}

// Run starts the worker's main loop.
func (w *Worker) Run(ctx context.Context) {
	log.Info().Int("worker_id", w.id).Msg("Worker started")

	for {
		select {
		case task := <-w.queue:
			w.executeTask(ctx, task)

			// Throttle to avoid IBKR rate limits
			time.Sleep(w.throttle)

		case <-ctx.Done():
			log.Info().Int("worker_id", w.id).Msg("Worker stopped")
			return
		}
	}
}

// executeTask processes a single task.
func (w *Worker) executeTask(ctx context.Context, task Task) {
	start := time.Now()

	// Create job record
	job := &model.BackgroundJob{
		Symbol:     task.Symbol,
		JobType:    task.Type,
		Status:     "running",
		StartedAt:  &start,
		RetryCount: 0,
		CreatedAt:  task.CreatedAt,
	}

	if task.RetryOf > 0 {
		// Load retry count from previous job
		prevJob, _ := w.store.GetBackgroundJob(task.RetryOf)
		if prevJob != nil {
			job.RetryCount = prevJob.RetryCount + 1
		}
	}

	jobID, err := w.store.CreateBackgroundJob(job)
	if err != nil {
		log.Error().Err(err).Str("symbol", task.Symbol).Msg("Failed to create job record")
		return
	}
	job.ID = jobID

	// Execute the actual refresh
	err = w.fetchAndCache(ctx, task.Symbol)

	duration := time.Since(start)
	now := time.Now()
	job.CompletedAt = &now

	if err != nil {
		w.handleFailure(job, err, duration)
	} else {
		w.handleSuccess(job, duration)
	}
}

// fetchAndCache connects to IBKR, fetches data, calls Python analysis, and saves to SQLite.
// This replicates the logic from webui.fetchLiveAnalysis() but runs in background.
func (w *Worker) fetchAndCache(ctx context.Context, symbol string) error {
	// Create broker with fallback (IBKR → yfinance)
	b := broker.NewWithFallback(ibkr.Config{
		Host:     w.ibCfg.Host,
		Port:     w.ibCfg.Port,
		ClientID: int64(10 + w.id),
	}, w.ibCfg.PythonBin)

	if err := b.Connect(ctx); err != nil {
		return fmt.Errorf("connect to broker: %w", err)
	}
	defer b.Disconnect()

	// Fetch market data
	svc := server.NewMarketDataService(b, w.store)
	stockData, err := server.FetchSymbolData(ctx, symbol, svc)
	if err != nil {
		return fmt.Errorf("fetch market data: %w", err)
	}

	// Connect to Python analysis engine
	analysisClient, err := analysis.NewClient(w.analysisCfg.Addr)
	if err != nil {
		return fmt.Errorf("connect analysis engine: %w", err)
	}
	defer analysisClient.Close()

	// Run analysis with timeout
	analyzeCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	protoResp, err := analysisClient.AnalyzeStock(analyzeCtx, &analysisv1.AnalyzeStockRequest{
		Symbol:           symbol,
		ForecastDays:     w.analysisCfg.ForecastDays,
		AvailableCapital: w.analysisCfg.Capital,
		RiskTolerance:    w.analysisCfg.RiskTolerance,
		HistoricalBars:   stockData.HistoricalBars,
		OptionChain:      stockData.OptionChain,
		CurrentQuote:     stockData.Quote,
	})
	if err != nil {
		return fmt.Errorf("analysis engine: %w", err)
	}

	// Convert proto response to full AnalyzeResponse (same as webui.fetchLiveAnalysis)
	resp := webui.ProtoToAnalyzeResponse(protoResp, symbol, true)

	payload, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshal analysis response: %w", err)
	}

	if err := w.store.SaveAnalysisCache(ctx, symbol, payload); err != nil {
		return fmt.Errorf("save analysis cache: %w", err)
	}

	// Save watchlist snapshot with all fields (including range forecasts)
	snap := model.QuickSummary{
		Symbol:      symbol,
		Price:       resp.Summary.Price,
		Trend:       resp.Technical.Trend,
		RSI:         resp.Technical.RSI14,
		IVRank:      resp.Options.IVRank,
		MaxPain:     resp.Options.MaxPain,
		PCR:         resp.Options.PCROi,
		RangeLow1S:  resp.Outlook.RangeLow1S,
		RangeHigh1S: resp.Outlook.RangeHigh1S,
	}

	if len(resp.Strategies) > 0 {
		snap.Recommendation = resp.Strategies[0].StrategyName
		snap.OpportunityScore = resp.Strategies[0].Score
	}

	if err := w.store.SaveWatchlistSnapshot(ctx, snap); err != nil {
		return fmt.Errorf("save watchlist snapshot: %w", err)
	}

	return nil
}

// handleSuccess marks the job as successful and logs.
func (w *Worker) handleSuccess(job *model.BackgroundJob, duration time.Duration) {
	job.Status = "success"
	if err := w.store.UpdateBackgroundJob(job); err != nil {
		log.Error().Err(err).Int64("job_id", job.ID).Msg("Failed to update job")
		return
	}

	log.Info().
		Str("symbol", job.Symbol).
		Dur("duration", duration).
		Msg("Background task completed")
}

// handleFailure marks the job as failed and schedules retry if applicable.
func (w *Worker) handleFailure(job *model.BackgroundJob, err error, duration time.Duration) {
	job.Status = "failed"
	job.ErrorMessage = err.Error()

	if err := w.store.UpdateBackgroundJob(job); err != nil {
		log.Error().Err(err).Int64("job_id", job.ID).Msg("Failed to update job")
		return
	}

	log.Error().
		Str("symbol", job.Symbol).
		Dur("duration", duration).
		Int("retry_count", job.RetryCount).
		Err(err).
		Msg("Background task failed")

	// Exponential backoff retry
	if job.RetryCount < 3 {
		delay := calculateRetryDelay(job.RetryCount + 1)

		time.AfterFunc(delay, func() {
			select {
			case w.retryQueue <- Task{
				Symbol:    job.Symbol,
				Type:      job.JobType,
				CreatedAt: time.Now(),
				RetryOf:   job.ID,
			}:
				log.Info().
					Str("symbol", job.Symbol).
					Dur("retry_delay", delay).
					Int("retry_count", job.RetryCount+1).
					Msg("Scheduled retry")
			default:
				log.Warn().Str("symbol", job.Symbol).Msg("Retry queue full, dropping retry")
			}
		})
	} else {
		log.Warn().
			Str("symbol", job.Symbol).
			Msg("Max retries exceeded, giving up")
	}
}

// calculateRetryDelay returns the retry delay based on retry count.
// 1st retry: 1 minute
// 2nd retry: 5 minutes
// 3rd retry: 15 minutes
func calculateRetryDelay(retryCount int) time.Duration {
	delays := []time.Duration{
		1 * time.Minute,
		5 * time.Minute,
		15 * time.Minute,
	}

	if retryCount-1 < len(delays) {
		return delays[retryCount-1]
	}
	return 15 * time.Minute
}
