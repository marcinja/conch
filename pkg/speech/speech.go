package speech

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/veandco/go-sdl2/sdl"
)

// DebugMode specifies what parts of the speech module to debug
type DebugMode uint

const (
	DebugNone       DebugMode = 0
	DebugCapture    DebugMode = 1 << iota // Audio capture debugging
	DebugTranscribe                       // Transcription debugging
	// Add more debug modes here as needed
	DebugAll DebugMode = 0xFFFFFFFF // Debug everything
)

const (
	// Audio configuration
	AudioFrequency  = 16000
	AudioFormat     = sdl.AUDIO_S16LSB
	AudioChannels   = 1
	AudioSamples    = 4096
	AudioBufferSize = 1024 * 1024 // 1MB buffer

	// Voice activity detection
	VadThreshold     = 100 // Threshold for detecting voice activity (much lower)
	VadSilenceFrames = 10  // Number of frames of silence to end recording (shorter pause)
)

// AudioCallback is called by SDL when more audio data is needed
type AudioCallback struct {
	buffer       []int16
	bufferSize   int
	mutex        sync.Mutex
	silentFrames int
	isActive     bool
}

// AudioData represents a captured audio segment
type AudioData struct {
	Samples    []int16
	SampleRate int
}

// SpeechService handles voice activity detection and transcription
type SpeechService struct {
	deviceID       sdl.AudioDeviceID
	callback       *AudioCallback
	isInitialized  bool
	isListening    bool
	isRecording    bool
	isTranscribing bool
	isShutdown     bool
	audioData      *AudioData

	// Events channels
	recordingStarted chan struct{}
	recordingStopped chan *AudioData

	// Control
	stopListening chan struct{}
	mutex         sync.Mutex

	// Debug settings
	debugMode DebugMode
}

// debugLog logs a message only if the specified debug mode is enabled
func (s *SpeechService) debugLog(mode DebugMode, format string, args ...interface{}) {
	if s.debugMode&mode != 0 {
		log.Printf("DEBUG: "+format, args...)
	}
}

// NewSpeechService creates a new speech service instance
func NewSpeechService() *SpeechService {
	// Check for debug environment variables
	var debugMode DebugMode = DebugNone
	debugEnv := os.Getenv("DEBUG")

	if debugEnv != "" {
		debugLower := strings.ToLower(debugEnv)

		// Parse specific debug modes
		if strings.Contains(debugLower, "capture") {
			debugMode |= DebugCapture
			log.Println("Debug capture enabled for speech module")
		}

		if strings.Contains(debugLower, "transcribe") {
			debugMode |= DebugTranscribe
			log.Println("Debug transcription enabled for speech module")
		}

		// Check for all debug mode
		if debugLower == "all" {
			debugMode = DebugAll
			log.Println("Debug mode fully enabled for speech module")
		}
	}

	return &SpeechService{
		recordingStarted: make(chan struct{}, 1),
		recordingStopped: make(chan *AudioData, 1),
		stopListening:    make(chan struct{}, 1),
		audioData: &AudioData{
			Samples:    make([]int16, 0, AudioBufferSize),
			SampleRate: AudioFrequency,
		},
		debugMode: debugMode,
	}
}

// WithDebug sets the debug mode for the speech service
func (s *SpeechService) WithDebug(mode DebugMode) *SpeechService {
	s.debugMode = mode
	if mode != DebugNone {
		log.Println("Debug logging set to mode:", mode)
	}
	return s
}

// Initialize sets up SDL2 audio capture
func (s *SpeechService) Initialize() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.isInitialized {
		return nil
	}

	if err := sdl.Init(sdl.INIT_AUDIO); err != nil {
		return fmt.Errorf("failed to initialize SDL audio: %v", err)
	}

	callback := &AudioCallback{
		buffer:     make([]int16, AudioBufferSize),
		bufferSize: 0,
		isActive:   false,
	}
	s.callback = callback

	spec := sdl.AudioSpec{
		Freq:     AudioFrequency,
		Format:   AudioFormat,
		Channels: AudioChannels,
		Samples:  AudioSamples,
		Callback: nil, // We'll use AudioDeviceID.QueueAudio instead
	}

	var obtainedSpec sdl.AudioSpec
	deviceID, err := sdl.OpenAudioDevice("", true, &spec, &obtainedSpec, sdl.AUDIO_ALLOW_ANY_CHANGE)
	if err != nil {
		return fmt.Errorf("failed to open audio device: %v", err)
	}

	s.deviceID = deviceID
	s.isInitialized = true
	log.Println("SDL audio initialized successfully")

	return nil
}

// StartListening begins monitoring for voice activity
func (s *SpeechService) StartListening() error {
	s.mutex.Lock()
	if !s.isInitialized {
		s.mutex.Unlock()
		return errors.New("speech service not initialized")
	}

	if s.isListening {
		s.mutex.Unlock()
		return errors.New("already listening")
	}

	s.isListening = true
	s.mutex.Unlock()

	// Start capturing in a separate goroutine
	go s.captureAudio()

	log.Println("Started listening for voice input")
	return nil
}

// StopListening ends voice monitoring
func (s *SpeechService) StopListening() error {
	s.mutex.Lock()

	if !s.isListening {
		s.mutex.Unlock()
		return errors.New("not currently listening")
	}

	// Set state first to prevent further audio processing
	s.isListening = false
	s.mutex.Unlock()

	// Signal the listening goroutine to stop
	select {
	case s.stopListening <- struct{}{}:
	default:
		// Channel already has a message or goroutine already exited
	}

	// Pause the audio device
	sdl.PauseAudioDevice(s.deviceID, true)

	log.Println("Stopped listening for voice input")
	return nil
}

// IsListening returns the current listening state
func (s *SpeechService) IsListening() bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.isListening
}

// IsRecording returns the current recording state
func (s *SpeechService) IsRecording() bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.isRecording
}

// IsTranscribing returns the current transcribing state
func (s *SpeechService) IsTranscribing() bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.isTranscribing
}

// SetTranscribing sets the transcribing state for status display
func (s *SpeechService) SetTranscribing(transcribing bool) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.isTranscribing = transcribing
}

// captureAudio continuously captures audio and detects voice activity
func (s *SpeechService) captureAudio() {
	// Start audio capture
	sdl.PauseAudioDevice(s.deviceID, false)

	buffer := make([]byte, AudioSamples*2) // 16-bit samples = 2 bytes per sample
	silenceFrames := 0
	isRecording := false

	// Use a lower VAD threshold for testing
	localVadThreshold := int64(VadThreshold) // Already lowered in the constant

	defer func() {
		// Ensure we always clean up properly
		s.mutex.Lock()
		s.isRecording = false
		s.isListening = false
		s.mutex.Unlock()

		sdl.PauseAudioDevice(s.deviceID, true)
		log.Println("Audio capture goroutine exited")
	}()

	for {
		// Check if we should stop
		select {
		case <-s.stopListening:
			return // Exit the goroutine
		default:
			// Continue capturing
		}

		// Check if still listening
		s.mutex.Lock()
		stillListening := s.isListening
		s.mutex.Unlock()

		if !stillListening {
			return // Exit the goroutine
		}

		// Read audio data
		bytesRead, err := sdl.DequeueAudio(s.deviceID, buffer)
		if err != nil {
			// Check if we're shutting down
			s.mutex.Lock()
			if !s.isListening {
				s.mutex.Unlock()
				return // Exit cleanly
			}
			s.mutex.Unlock()

			log.Printf("Error reading audio: %v", err)
			time.Sleep(100 * time.Millisecond) // Wait a bit before retrying
			continue
		}

		// Skip if no data
		if bytesRead == 0 {
			time.Sleep(10 * time.Millisecond)
			continue
		}

		// Debug - always show audio data being received
		s.debugLog(DebugCapture, "Audio bytes read: %d", bytesRead)

		// Convert bytes to int16 samples - only convert the actual bytes read
		numSamples := bytesRead / 2 // 2 bytes per sample
		samples := make([]int16, numSamples)
		for i := 0; i < int(numSamples); i++ {
			samples[i] = int16(buffer[i*2]) | (int16(buffer[i*2+1]) << 8)
		}

		// Calculate average energy for debug
		var sum int64
		for _, sample := range samples {
			value := int64(sample)
			if value < 0 {
				value = -value
			}
			sum += value
		}
		average := sum / int64(len(samples))

		// Print audio level for debugging
		s.debugLog(DebugCapture, "Audio level: %d (threshold: %d)", average, localVadThreshold)

		// Detect voice activity
		if !isRecording {
			if average > localVadThreshold {
				// Voice detected, start recording
				isRecording = true
				s.mutex.Lock()
				s.isRecording = true
				s.audioData.Samples = s.audioData.Samples[:0] // Clear buffer
				s.mutex.Unlock()

				// Notify that recording has started
				select {
				case s.recordingStarted <- struct{}{}:
				default:
					// Channel full, skip
				}

				s.debugLog(DebugCapture, "Voice detected (level: %d), started recording", average)
			}
		}

		if isRecording {
			// Add samples to buffer
			s.mutex.Lock()
			s.audioData.Samples = append(s.audioData.Samples, samples...)
			s.mutex.Unlock()

			// Check for end of speech
			if average > localVadThreshold {
				silenceFrames = 0
			} else {
				silenceFrames++
				if silenceFrames >= VadSilenceFrames {
					// Silence detected for long enough, stop recording
					isRecording = false
					s.mutex.Lock()
					s.isRecording = false

					// Only process if we got enough data
					if len(s.audioData.Samples) > AudioFrequency/4 { // At least 0.25s of audio
						// Create a copy of the audio data
						audioData := &AudioData{
							Samples:    make([]int16, len(s.audioData.Samples)),
							SampleRate: s.audioData.SampleRate,
						}
						copy(audioData.Samples, s.audioData.Samples)
						s.mutex.Unlock()

						// Notify that recording has stopped with the captured audio
						select {
						case s.recordingStopped <- audioData:
							log.Printf("End of speech detected (recorded %d samples), stopped recording",
								len(audioData.Samples))
						default:
							log.Println("Warning: recordingStopped channel full, dropping audio")
						}
					} else {
						s.mutex.Unlock()
						log.Println("Recording too short, discarded")
					}

					silenceFrames = 0
				}
			}
		}

		// Small sleep to prevent consuming 100% CPU
		time.Sleep(10 * time.Millisecond)
	}
}

// detectVoice implements a simple voice activity detection algorithm
func detectVoice(samples []int16) bool {
	// Calculate average energy
	var sum int64
	for _, sample := range samples {
		// Take absolute value
		value := int64(sample)
		if value < 0 {
			value = -value
		}
		sum += value
	}

	average := sum / int64(len(samples))
	return average > VadThreshold
}

// WaitForRecording blocks until speech is detected and recorded
func (s *SpeechService) WaitForRecording() (*AudioData, error) {
	// Check if we're shutting down or not listening
	s.mutex.Lock()
	if s.isShutdown {
		s.mutex.Unlock()
		return nil, errors.New("service is shutting down")
	}

	if !s.isListening {
		s.mutex.Unlock()
		return nil, errors.New("not currently listening")
	}
	s.mutex.Unlock()

	// Short timeout to allow for checking status periodically
	timeoutDuration := 1 * time.Second

	for {
		// Check shutdown state first
		s.mutex.Lock()
		if s.isShutdown {
			s.mutex.Unlock()
			return nil, errors.New("service is shutting down")
		}
		s.mutex.Unlock()

		// Wait for a recording with a short timeout
		select {
		case audioData := <-s.recordingStopped:
			return audioData, nil
		case <-time.After(timeoutDuration):
			// Check shutdown state again
			s.mutex.Lock()
			if s.isShutdown {
				s.mutex.Unlock()
				return nil, errors.New("service is shutting down")
			}

			// Then check listening state
			if !s.isListening {
				s.mutex.Unlock()
				return nil, errors.New("listening stopped")
			}
			s.mutex.Unlock()

			// Just a timeout, continue waiting
			continue
		}
	}
}

// Cleanup releases SDL resources
func (s *SpeechService) Cleanup() error {
	log.Println("Starting SpeechService cleanup...")

	// Mark as shutting down first thing
	s.mutex.Lock()
	s.isShutdown = true
	s.mutex.Unlock()

	// First stop listening if needed, but don't hold the main lock during this
	listening := s.IsListening()
	if listening {
		// StopListening will acquire/release its own mutex
		log.Println("Stopping listening for voice input")
		s.StopListening()
		// Small wait to allow the goroutine to exit
		time.Sleep(200 * time.Millisecond)
	}

	// Clean up SDL resources
	s.mutex.Lock()
	isInit := s.isInitialized
	deviceID := s.deviceID
	s.isInitialized = false // Prevent reuse
	s.mutex.Unlock()

	if isInit {
		// Close audio device if it's valid
		if deviceID > 0 {
			sdl.PauseAudioDevice(deviceID, true)
			sdl.CloseAudioDevice(deviceID)
		}
		sdl.Quit()
		log.Println("SDL audio resources released")
	}

	log.Println("SpeechService cleanup completed")
	return nil
}

// Name returns the service name for shutdown management
func (s *SpeechService) Name() string {
	return "SpeechService"
}

// Shutdown implements the Shutdownable interface
func (s *SpeechService) Shutdown() error {
	return s.Cleanup()
}

// audioCallback is the C-compatible callback function for SDL
func audioCallback(userdata unsafe.Pointer, stream *uint8, length int32) {
	callback := (*AudioCallback)(userdata)
	callback.mutex.Lock()
	defer callback.mutex.Unlock()

	// Cast stream to a slice of int16
	samples := unsafe.Slice((*int16)(unsafe.Pointer(stream)), length/2)

	// Check voice activity
	var sum int64
	for _, sample := range samples {
		value := int64(sample)
		if value < 0 {
			value = -value
		}
		sum += value
	}

	average := sum / int64(len(samples))

	// Track voice activity
	if average > VadThreshold {
		callback.isActive = true
		callback.silentFrames = 0
	} else if callback.isActive {
		callback.silentFrames++
		if callback.silentFrames > VadSilenceFrames {
			callback.isActive = false
		}
	}

	// Copy audio data to buffer if active or recently active
	if callback.isActive {
		// Ensure we don't overflow the buffer
		if callback.bufferSize+len(samples) > len(callback.buffer) {
			// Buffer full, drop oldest data
			copy(callback.buffer, callback.buffer[len(samples):callback.bufferSize])
			callback.bufferSize -= len(samples)
		}

		// Copy new samples to buffer
		copy(callback.buffer[callback.bufferSize:], samples)
		callback.bufferSize += len(samples)
	}
}
