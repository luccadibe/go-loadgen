package go_loadgen

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/csv"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// CSVSerializable is a struct that can be serialized to CSV
type CSVSerializable interface {
	// CSVHeaders returns the headers that should be used for a CSV file.
	CSVHeaders() []string
	// CSVRecord returns the record that should be used to store the struct as a row in a CSV file.
	CSVRecord() []string
}

// CSVCollector can collect results and write them to a CSV file. It requires result types to implement CSVSerializable. It will write the headers on the first collect and then every flushInterval. Note that headers will be rewritten if a new collector is created.
type CSVCollector[R CSVSerializable] struct {
	writer        *csv.Writer
	file          *os.File
	flushInterval time.Duration
	filePath      string
	headerWritten bool
	mu            sync.Mutex
	ctx           context.Context
	cancel        context.CancelFunc
}

// NewCSVCollector creates a new CSV collector and starts a goroutine to flush the collector every flushInterval.
func NewCSVCollector[R CSVSerializable](filePath string, flushInterval time.Duration) (*CSVCollector[R], error) {
	if flushInterval <= 0 {
		return nil, fmt.Errorf("flush interval must be positive")
	}
	file, err := os.Create(filePath)
	if err != nil {
		return nil, err
	}

	c := &CSVCollector[R]{
		writer:        csv.NewWriter(file),
		file:          file,
		flushInterval: flushInterval,
		filePath:      filePath,
		headerWritten: false,
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.ctx, c.cancel = ctx, cancel

	go c.RunFlush(ctx)

	return c, nil
}

// Collect collects a result and writes it to the CSV file.
func (c *CSVCollector[R]) Collect(result R) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Write header on first collect
	if !c.headerWritten {
		headers := result.CSVHeaders()
		if err := c.writer.Write(headers); err != nil {
			fmt.Printf("Error writing CSV header: %v\n", err)
			return
		}
		c.headerWritten = true
	}

	record := result.CSVRecord()
	if err := c.writer.Write(record); err != nil {
		fmt.Printf("Error writing CSV record: %v\n", err)
	}
}

// Close flushes the CSV collector and closes the file.
func (c *CSVCollector[R]) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cancel()
	c.writer.Flush()
	if c.file != nil {
		c.file.Close()
	}
}

// RunFlush flushes the CSV collector every flushInterval.
func (c *CSVCollector[R]) RunFlush(ctx context.Context) {
	t := time.NewTicker(c.flushInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.mu.Lock()
			c.writer.Flush()
			c.mu.Unlock()
		}
	}
}

// GobCollectorOption configures a GobCollector.
type GobCollectorOption func(*gobCollectorConfig)

type gobCollectorConfig struct {
	bufferSize       int
	gzipEnabled      bool
	compressionLevel int
}

// WithGobCollectorBufferSize configures how many results can queue before Collect blocks.
func WithGobCollectorBufferSize(size int) GobCollectorOption {
	return func(cfg *gobCollectorConfig) {
		if size > 0 {
			cfg.bufferSize = size
		}
	}
}

// WithGobCollectorGzip enables gzip compression. Use gzip.BestSpeed for better
// write throughput, or gzip.BestCompression for lower storage cost.
func WithGobCollectorGzip(level int) GobCollectorOption {
	return func(cfg *gobCollectorConfig) {
		cfg.gzipEnabled = true
		cfg.compressionLevel = level
	}
}

var errGobCollectorClosed = errors.New("gob collector is closed")

// GobCollector stores results as an async gob stream. It is a good default for
// very large experiments where CSV conversion and writer lock contention are too expensive.
type GobCollector[R any] struct {
	file          *os.File
	buf           *bufio.Writer
	gzipWriter    *gzip.Writer
	encoder       *gob.Encoder
	results       chan R
	flushInterval time.Duration
	collectMu     sync.Mutex
	collectWG     sync.WaitGroup
	closed        bool
	closeOnce     sync.Once
	done          chan struct{}
	errMu         sync.Mutex
	err           error
}

// NewGobCollector creates a collector that writes results as a gob stream.
func NewGobCollector[R any](filePath string, flushInterval time.Duration, opts ...GobCollectorOption) (*GobCollector[R], error) {
	if flushInterval <= 0 {
		return nil, fmt.Errorf("flush interval must be positive")
	}

	cfg := gobCollectorConfig{
		bufferSize:       4096,
		compressionLevel: gzip.NoCompression,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	file, err := os.Create(filePath)
	if err != nil {
		return nil, err
	}

	buf := bufio.NewWriterSize(file, 256*1024)
	var writer io.Writer = buf
	var gzipWriter *gzip.Writer
	if cfg.gzipEnabled {
		gzipWriter, err = gzip.NewWriterLevel(buf, cfg.compressionLevel)
		if err != nil {
			file.Close()
			return nil, err
		}
		writer = gzipWriter
	}

	c := &GobCollector[R]{
		file:          file,
		buf:           buf,
		gzipWriter:    gzipWriter,
		encoder:       gob.NewEncoder(writer),
		results:       make(chan R, cfg.bufferSize),
		flushInterval: flushInterval,
		done:          make(chan struct{}),
	}

	go c.run()

	return c, nil
}

// Collect queues a result to be written by the collector's writer goroutine.
func (c *GobCollector[R]) Collect(result R) {
	c.collectMu.Lock()
	if c.closed {
		c.collectMu.Unlock()
		c.setErr(errGobCollectorClosed)
		return
	}
	c.collectWG.Add(1)
	c.collectMu.Unlock()

	defer c.collectWG.Done()
	c.results <- result
}

// Close drains queued results, flushes the gob stream, and closes the file.
func (c *GobCollector[R]) Close() {
	c.closeOnce.Do(func() {
		c.collectMu.Lock()
		c.closed = true
		c.collectMu.Unlock()
		c.collectWG.Wait()
		close(c.results)
		<-c.done
	})
}

// CloseAndErr closes the collector and returns the first asynchronous write,
// flush, close, or post-close collection error observed by the collector.
func (c *GobCollector[R]) CloseAndErr() error {
	c.Close()
	return c.Err()
}

// Err returns the first asynchronous write, flush, close, or post-close
// collection error observed by the collector.
func (c *GobCollector[R]) Err() error {
	c.errMu.Lock()
	defer c.errMu.Unlock()
	return c.err
}

func (c *GobCollector[R]) run() {
	defer close(c.done)
	t := time.NewTicker(c.flushInterval)
	defer t.Stop()

	for {
		select {
		case result, ok := <-c.results:
			if !ok {
				c.flushAndClose()
				return
			}
			if err := c.encoder.Encode(result); err != nil {
				c.setErr(err)
			}
		case <-t.C:
			c.flush()
		}
	}
}

func (c *GobCollector[R]) flush() {
	if c.gzipWriter != nil {
		if err := c.gzipWriter.Flush(); err != nil {
			c.setErr(err)
		}
	}
	if err := c.buf.Flush(); err != nil {
		c.setErr(err)
	}
}

func (c *GobCollector[R]) flushAndClose() {
	if c.gzipWriter != nil {
		if err := c.gzipWriter.Close(); err != nil {
			c.setErr(err)
		}
	}
	if err := c.buf.Flush(); err != nil {
		c.setErr(err)
	}
	if err := c.file.Close(); err != nil {
		c.setErr(err)
	}
}

func (c *GobCollector[R]) setErr(err error) {
	if err == nil {
		return
	}
	c.errMu.Lock()
	defer c.errMu.Unlock()
	if c.err == nil {
		c.err = err
		fmt.Printf("Error writing gob record: %v\n", err)
	}
}
