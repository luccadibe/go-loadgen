package go_loadgen

import (
	"context"
	"encoding/csv"
	"fmt"
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
