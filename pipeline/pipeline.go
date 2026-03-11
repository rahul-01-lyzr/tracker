package pipeline

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"lyzr-tracker/models"
	"lyzr-tracker/tracker"
)

// Pipeline is a lock-free, channel-based event pipeline that:
//   - Accepts events via a buffered channel (non-blocking for producers)
//   - Runs N worker goroutines that accumulate events into batches
//   - Flushes batches to Mixpanel either when batch is full OR on a timer

type Pipeline struct {
	eventCh   chan models.InternalEvent
	mp        *tracker.MixpanelTracker
	batchSize int
	flushInt  time.Duration
	workers   int
	wg        sync.WaitGroup
	ctx       context.Context
	cancel    context.CancelFunc

	enqueued uint64
	flushed  uint64
	dropped  uint64
	mu       sync.Mutex
}

// New creates and returns a new Pipeline. Call Start() to begin processing.
func New(mp *tracker.MixpanelTracker, channelSize, batchSize, workers int, flushInterval time.Duration) *Pipeline {
	ctx, cancel := context.WithCancel(context.Background())
	return &Pipeline{
		eventCh:   make(chan models.InternalEvent, channelSize),
		mp:        mp,
		batchSize: batchSize,
		flushInt:  flushInterval,
		workers:   workers,
		ctx:       ctx,
		cancel:    cancel,
	}
}

// Enqueue adds an event to the pipeline. Returns false if channel is full.
func (p *Pipeline) Enqueue(evt models.InternalEvent) bool {
	select {
	case p.eventCh <- evt:
		atomic.AddUint64(&p.enqueued, 1)
		return true
	default:
		atomic.AddUint64(&p.dropped, 1)
		log.Printf("WARN: pipeline channel full, event dropped: %s", evt.Event)
		return false
	}
}

// EnqueueBatch enqueues multiple events. Returns count of successfully enqueued events.
func (p *Pipeline) EnqueueBatch(events []models.InternalEvent) int {
	enqueued := 0
	for i := range events {
		if p.Enqueue(events[i]) {
			enqueued++
		}
	}
	return enqueued
}

// Start launches worker goroutines.
func (p *Pipeline) Start() {
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}
	log.Printf("INFO: pipeline started with %d workers, batch_size=%d, flush_interval=%s, channel_buffer=%d",
		p.workers, p.batchSize, p.flushInt, cap(p.eventCh))
}

// Shutdown gracefully drains remaining events and stops workers.
func (p *Pipeline) Shutdown() {
	log.Println("INFO: pipeline shutting down, draining remaining events...")
	p.cancel()
	close(p.eventCh) // signal workers to drain and exit
	p.wg.Wait()
	fl := uint64(0)
	p.mu.Lock()
	fl = p.flushed
	p.mu.Unlock()
	log.Printf("INFO: pipeline shutdown complete. total_flushed=%d total_dropped=%d", fl, atomic.LoadUint64(&p.dropped))
}

// Stats returns pipeline metrics.
func (p *Pipeline) Stats() (enqueued, flushed, dropped uint64, channelLen int) {
	p.mu.Lock()
	flushed = p.flushed
	channelLen = len(p.eventCh)
	p.mu.Unlock()
	return atomic.LoadUint64(&p.enqueued), flushed, atomic.LoadUint64(&p.dropped), channelLen
}

// worker is the core loop for each background goroutine.
func (p *Pipeline) worker(id int) {
	defer p.wg.Done()

	batch := make([]models.InternalEvent, 0, p.batchSize)
	timer := time.NewTimer(p.flushInt)
	defer timer.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		// Copy batch to avoid data races on the slice
		toSend := make([]models.InternalEvent, len(batch))
		copy(toSend, batch)
		batch = batch[:0] // reset without re-allocating

		if err := p.mp.SendBatch(context.Background(), toSend); err != nil {
			log.Printf("ERROR: worker-%d: mixpanel flush failed (%d events, Retrying): %v", id, len(toSend), err)
			p.mp.SendBatch(context.Background(), toSend)
		} else {
			p.mu.Lock()
			p.flushed += uint64(len(toSend))
			p.mu.Unlock()
		}
	}

	for {
		select {
		case evt, ok := <-p.eventCh:
			if !ok {
				// Channel closed — flush remaining and exit
				flush()
				return
			}
			batch = append(batch, evt)
			if len(batch) >= p.batchSize {
				flush()
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(p.flushInt)
			}

		case <-timer.C:
			// Time-based flush — ensures events don't sit in buffer too long
			flush()
			timer.Reset(p.flushInt)
		}
	}
}
