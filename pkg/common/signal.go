package common

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// Shutdownable interface for services that can be shut down gracefully
type Shutdownable interface {
	Name() string
	Shutdown() error
}

// GracefulShutdown handles OS signals and manages shutting down registered services
type GracefulShutdown struct {
	services       []Shutdownable
	sigCh          chan os.Signal
	timeout        time.Duration
	mu             sync.Mutex
	preShutdownCbs []func()
}

// NewGracefulShutdown creates a shutdown manager with signal handling
func NewGracefulShutdown(timeout time.Duration) *GracefulShutdown {
	return &GracefulShutdown{
		services:       make([]Shutdownable, 0),
		sigCh:          make(chan os.Signal, 1),
		timeout:        timeout,
		preShutdownCbs: make([]func(), 0),
	}
}

// Register adds a service for graceful shutdown
func (gs *GracefulShutdown) Register(svc Shutdownable) {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	gs.services = append(gs.services, svc)
	log.Printf("Registered service for shutdown: %s", svc.Name())
}

// Start begins listening for termination signals
func (gs *GracefulShutdown) Start() {
	// Set up signal notification
	signal.Notify(gs.sigCh, os.Interrupt, syscall.SIGTERM)

	// Start a goroutine to handle signals
	go func() {
		sig := <-gs.sigCh
		log.Printf("Received signal: %v, initiating graceful shutdown", sig)
		gs.shutdown()
	}()
}

// SetSignalHandler allows using a custom signal channel
func (gs *GracefulShutdown) SetSignalHandler(sigCh chan os.Signal) {
	// Reset the existing notification
	signal.Stop(gs.sigCh)
	
	// Use the new channel
	gs.sigCh = sigCh
	signal.Notify(gs.sigCh, os.Interrupt, syscall.SIGTERM)
}

// StartShutdown initiates the shutdown process programmatically
func (gs *GracefulShutdown) StartShutdown() {
	log.Println("Manual shutdown initiated")
	gs.shutdown()
}

// shutdown performs the actual shutdown process
func (gs *GracefulShutdown) shutdown() {
	gs.mu.Lock()
	svcs := make([]Shutdownable, len(gs.services))
	copy(svcs, gs.services)
	gs.mu.Unlock()

	if len(svcs) == 0 {
		return
	}

	log.Printf("Shutting down %d services...", len(svcs))

	// Create a context with timeout for the entire shutdown process
	ctx, cancel := context.WithTimeout(context.Background(), gs.timeout)
	defer cancel()

	// Create waitgroup to track completion
	var wg sync.WaitGroup
	wg.Add(len(svcs))

	// Shutdown each service in parallel
	for _, svc := range svcs {
		go func(s Shutdownable) {
			defer wg.Done()

			log.Printf("Shutting down %s...", s.Name())
			err := s.Shutdown()
			if err != nil {
				log.Printf("Error shutting down %s: %v", s.Name(), err)
			} else {
				log.Printf("Successfully shut down %s", s.Name())
			}
		}(svc)
	}

	// Wait for all services to finish or timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Println("All services shut down successfully")
	case <-ctx.Done():
		log.Printf("Shutdown timed out after %v", gs.timeout)
	}
}
