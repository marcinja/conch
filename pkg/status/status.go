package status

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/marcinja/conch/pkg/speech"
)

// StatusService manages the status display
type StatusService struct {
	svc    *speech.SpeechService
	done   chan struct{}
	writer io.Writer
}

// NewStatusService creates a new status service
func NewStatusService(speechSvc *speech.SpeechService) *StatusService {
	return NewStatusServiceWithWriter(speechSvc, os.Stdout)
}

// NewStatusServiceWithWriter creates a new status service with a custom writer
func NewStatusServiceWithWriter(speechSvc *speech.SpeechService, writer io.Writer) *StatusService {
	return &StatusService{
		svc:    speechSvc,
		done:   make(chan struct{}),
		writer: writer,
	}
}

// Start begins the status service
func (s *StatusService) Start() {
	// Status display goroutine
	go func() {
		for {
			select {
			case <-s.done:
				fmt.Fprint(s.writer, "\rStatus: SHUTDOWN 🛑\n")
				return
			default:
				if s.svc.IsRecording() {
					fmt.Fprint(s.writer, "\rStatus: RECORDING 🔴 ")
				} else if s.svc.IsTranscribing() {
					fmt.Fprint(s.writer, "\rStatus: TRANSCRIBING 🔄 ")
				} else if s.svc.IsListening() {
					fmt.Fprint(s.writer, "\rStatus: LISTENING 🔊 ")
				} else {
					fmt.Fprint(s.writer, "\rStatus: IDLE ⏸️  ")
				}
				time.Sleep(100 * time.Millisecond)
			}
		}
	}()
}

// Name returns the service name
func (s *StatusService) Name() string {
	return "StatusService"
}

// Shutdown stops the status service
func (s *StatusService) Shutdown() error {
	close(s.done)
	return nil
}