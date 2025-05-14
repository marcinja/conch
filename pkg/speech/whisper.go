package speech

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// WhisperServerConfig contains configuration for the whisper server
type WhisperServerConfig struct {
	ModelPath      string  // Path to the whisper model file
	ServerPath     string  // Path to the whisper-server executable
	Host           string  // Host to bind the server to
	Port           int     // Port to bind the server to
	NumThreads     int     // Number of threads to use
	Language       string  // Language code (e.g. "en", "auto" for auto-detection)
	Translate      bool    // Whether to translate to English
	BeamSize       int     // Beam size for beam search
	BestOf         int     // Number of best candidates to keep
	WordThold      float64 // Word timestamp probability threshold
	PrintProgress  bool    // Print progress information
	PrintSpecial   bool    // Print special tokens
	NoTimestamps   bool    // Disable printing timestamps
	InitialPrompt  string  // Initial prompt for the model
	Temperature    float64 // Initial temperature for sampling
	TemperatureInc float64 // Temperature increment for fallbacks
}

// NewDefaultWhisperServerConfig creates a new WhisperServerConfig with default settings
func NewDefaultWhisperServerConfig() *WhisperServerConfig {
	// Use the user's home directory for the model path
	homedir, _ := os.UserHomeDir()
	
	// Default paths
	defaultModelPath := filepath.Join(homedir, "dev/whisper.cpp/models/ggml-large-v3-turbo.bin")
	defaultServerPath := filepath.Join(homedir, "dev/whisper.cpp/build/bin/whisper-server")
	
	// Override defaults with environment variables if set
	modelPath := getEnvOrDefault("WHISPER_MODEL", defaultModelPath)
	serverPath := getEnvOrDefault("WHISPER_BIN", defaultServerPath)

	return &WhisperServerConfig{
		ModelPath:      modelPath, // Path to model (configurable via env var)
		ServerPath:     serverPath, // Path to whisper-server (configurable via env var)
		Host:           "127.0.0.1",
		Port:           8080,
		NumThreads:     4,
		Language:       "en",
		Translate:      false,
		BeamSize:       5,
		BestOf:         2,
		WordThold:      0.01,
		PrintProgress:  false,
		PrintSpecial:   false,
		NoTimestamps:   false,
		InitialPrompt:  "",
		Temperature:    0.0, // Default to greedy decoding
		TemperatureInc: 0.2, // Default increment for fallbacks
	}
}

// getEnvOrDefault returns the value of the environment variable or the default value
func getEnvOrDefault(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists && value != "" {
		return value
	}
	return defaultValue
}

// WhisperServerResult represents the result of a transcription
type WhisperServerResult struct {
	Text     string           `json:"text"`
	Segments []WhisperSegment `json:"segments,omitempty"`
	Language string           `json:"language,omitempty"`
	Success  bool
}

// WhisperSegment represents a segment of transcribed audio
type WhisperSegment struct {
	ID         int     `json:"id"`
	Start      float64 `json:"start"`
	End        float64 `json:"end"`
	Text       string  `json:"text"`
	Tokens     []int   `json:"tokens,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
}

// WhisperServerService handles transcription using a local whisper.cpp server
type WhisperServerService struct {
	config     *WhisperServerConfig
	cmd        *exec.Cmd
	serverURL  string
	isRunning  bool
	debugMode  DebugMode
	mutex      sync.Mutex
	startTime  time.Time
	maxRetries int
}

// NewWhisperServerService creates a new WhisperServerService with default configuration
func NewWhisperServerService() *WhisperServerService {
	return &WhisperServerService{
		config:     NewDefaultWhisperServerConfig(),
		isRunning:  false,
		maxRetries: 3,
	}
}

// WithConfig sets the configuration for the WhisperServerService
func (s *WhisperServerService) WithConfig(config *WhisperServerConfig) *WhisperServerService {
	s.config = config
	return s
}

// WithDebug sets the debug mode for the WhisperServerService
func (s *WhisperServerService) WithDebug(mode DebugMode) *WhisperServerService {
	s.debugMode = mode
	return s
}

// debugLog logs a message if the specified debug mode is enabled
func (s *WhisperServerService) debugLog(mode DebugMode, format string, args ...interface{}) {
	if s.debugMode&mode != 0 {
		log.Printf("DEBUG [Whisper]: "+format, args...)
	}
}

// Initialize starts the whisper server and loads the model
func (s *WhisperServerService) Initialize() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.isRunning {
		return nil
	}

	// Check if server executable exists
	if _, err := os.Stat(s.config.ServerPath); err != nil {
		return fmt.Errorf("whisper-server executable not found at %s: %v", s.config.ServerPath, err)
	}
	// Construct server URL
	s.serverURL = fmt.Sprintf("http://%s:%d", s.config.Host, s.config.Port)

	// Build command arguments - include model path directly in server startup
	args := []string{
		"--host", s.config.Host,
		"--port", strconv.Itoa(s.config.Port),
		"-t", strconv.Itoa(s.config.NumThreads),
		"-m", s.config.ModelPath,
	}

	if s.config.PrintProgress {
		args = append(args, "-pp")
	}

	// Create and start the command
	s.cmd = exec.Command(s.config.ServerPath, args...)
	log.Printf("Starting whisper server with PID: [pending], command: %s %s", s.config.ServerPath, strings.Join(args, " "))
	s.debugLog(DebugTranscribe, "Starting whisper server: %s %s", s.config.ServerPath, strings.Join(args, " "))

	// Create log file for whisper server output
	logFile, err := os.Create("whisper-server.log")
	if err != nil {
		return fmt.Errorf("failed to create whisper server log file: %v", err)
	}

	// Write to both log file and buffer
	var stderr, stdout bytes.Buffer
	stderrWriter := io.MultiWriter(logFile, &stderr)
	stdoutWriter := io.MultiWriter(logFile, &stdout)

	s.cmd.Stdout = stdoutWriter
	s.cmd.Stderr = stderrWriter

	// Start the server
	if err := s.cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("failed to start whisper server: %v", err)
	}

	// Log process ID
	log.Printf("Whisper server started with PID: %d", s.cmd.Process.Pid)

	// Mark as running
	s.isRunning = true
	s.startTime = time.Now()

	// Monitor process in background
	go func() {
		err := s.cmd.Wait()
		s.mutex.Lock()
		if s.isRunning {
			if err != nil {
				log.Printf("Whisper server process exited with error: %v", err)
				log.Printf("See whisper-server.log for details")
			} else {
				log.Printf("Whisper server process exited")
			}
			s.isRunning = false
		}
		logFile.Close()
		s.mutex.Unlock()
	}()

	// Wait for the server to be ready
	s.debugLog(DebugTranscribe, "Waiting for server to be ready at %s", s.serverURL)
	var serverReady bool
	for i := 0; i < 30; i++ {
		time.Sleep(100 * time.Millisecond)

		// Check if the process is still running
		if s.cmd.ProcessState != nil && s.cmd.ProcessState.Exited() {
			errMsg := stderr.String()
			if errMsg == "" {
				errMsg = stdout.String()
			}
			return fmt.Errorf("whisper server exited unexpectedly: %s", errMsg)
		}

		// Try to connect to the server
		_, err := http.Get(s.serverURL)
		if err == nil {
			serverReady = true
			s.debugLog(DebugTranscribe, "Whisper server ready after %v", time.Since(s.startTime))
			break
		}
	}

	if !serverReady {
		s.Cleanup()
		return fmt.Errorf("whisper server failed to start in time")
	}

	s.debugLog(DebugTranscribe, "Server ready with model: %s", s.config.ModelPath)
	return nil
}

// loadModel loads a model into the running server
func (s *WhisperServerService) loadModel(modelPath string) error {
	// Prepare the multipart form
	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)

	// Add the model path field
	if err := writer.WriteField("model", modelPath); err != nil {
		return fmt.Errorf("failed to write model path field: %v", err)
	}

	// Add other config parameters
	if err := writer.WriteField("language", s.config.Language); err != nil {
		return fmt.Errorf("failed to write language field: %v", err)
	}

	// Close the writer
	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to close multipart writer: %v", err)
	}

	// Create the HTTP request
	loadURL := fmt.Sprintf("%s/load", s.serverURL)
	req, err := http.NewRequest("POST", loadURL, &requestBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Send the request
	s.debugLog(DebugTranscribe, "Sending load request to server")
	client := &http.Client{
		Timeout: 60 * time.Second, // Loading can take time
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send load request: %v", err)
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned error status %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// saveWavFile saves audio data to a temporary WAV file
func saveWavFile(samples []int16, sampleRate int) (string, error) {
	// Create a temporary file
	file, err := os.CreateTemp("", "whisper_*.wav")
	if err != nil {
		return "", err
	}
	defer file.Close()

	// WAV header structure
	type wavHeader struct {
		ChunkID       [4]byte // "RIFF"
		ChunkSize     uint32  // 36 + SubChunk2Size
		Format        [4]byte // "WAVE"
		SubChunk1ID   [4]byte // "fmt "
		SubChunk1Size uint32  // 16 for PCM
		AudioFormat   uint16  // 1 for PCM
		NumChannels   uint16  // 1 for mono, 2 for stereo
		SampleRate    uint32  // 16000, 44100, etc.
		ByteRate      uint32  // SampleRate * NumChannels * BitsPerSample/8
		BlockAlign    uint16  // NumChannels * BitsPerSample/8
		BitsPerSample uint16  // 8, 16, etc.
		SubChunk2ID   [4]byte // "data"
		SubChunk2Size uint32  // NumSamples * NumChannels * BitsPerSample/8
	}

	// Calculate sizes
	dataSize := uint32(len(samples) * 2) // 16-bit samples = 2 bytes per sample

	// Create header
	header := wavHeader{
		ChunkID:       [4]byte{'R', 'I', 'F', 'F'},
		ChunkSize:     36 + dataSize,
		Format:        [4]byte{'W', 'A', 'V', 'E'},
		SubChunk1ID:   [4]byte{'f', 'm', 't', ' '},
		SubChunk1Size: 16,
		AudioFormat:   1, // PCM
		NumChannels:   1, // Mono
		SampleRate:    uint32(sampleRate),
		ByteRate:      uint32(sampleRate * 1 * 16 / 8),
		BlockAlign:    2, // 1 channel * 16 bits per sample / 8
		BitsPerSample: 16,
		SubChunk2ID:   [4]byte{'d', 'a', 't', 'a'},
		SubChunk2Size: dataSize,
	}

	// Write header
	if err := binary.Write(file, binary.LittleEndian, header); err != nil {
		return "", err
	}

	// Write audio data
	if err := binary.Write(file, binary.LittleEndian, samples); err != nil {
		return "", err
	}

	return file.Name(), nil
}

// Transcribe sends audio data to the whisper server for transcription
func (s *WhisperServerService) Transcribe(audioData *AudioData) (*WhisperServerResult, error) {
	s.mutex.Lock()
	if !s.isRunning {
		s.mutex.Unlock()
		return nil, errors.New("whisper server not running")
	}
	s.mutex.Unlock()

	if audioData == nil || len(audioData.Samples) == 0 {
		return nil, errors.New("no audio data to transcribe")
	}

	// Save audio to a temporary WAV file
	wavFile, err := saveWavFile(audioData.Samples, audioData.SampleRate)
	if err != nil {
		return nil, fmt.Errorf("failed to save audio data: %v", err)
	}
	defer os.Remove(wavFile) // Clean up the temporary file when done

	s.debugLog(DebugTranscribe, "Saved audio to temporary file: %s", wavFile)

	// Prepare the multipart form
	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)

	// Add the file
	file, err := os.Open(wavFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open temporary file: %v", err)
	}
	defer file.Close()

	part, err := writer.CreateFormFile("file", filepath.Base(wavFile))
	if err != nil {
		return nil, fmt.Errorf("failed to create form file: %v", err)
	}

	if _, err := io.Copy(part, file); err != nil {
		return nil, fmt.Errorf("failed to copy file data: %v", err)
	}

	// Add other form fields
	writer.WriteField("temperature", fmt.Sprintf("%.1f", s.config.Temperature))
	writer.WriteField("temperature_inc", fmt.Sprintf("%.1f", s.config.TemperatureInc))
	writer.WriteField("response_format", "json")

	// Close the writer
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close multipart writer: %v", err)
	}

	// Create the HTTP request
	inferenceURL := fmt.Sprintf("%s/inference", s.serverURL)
	req, err := http.NewRequest("POST", inferenceURL, &requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Send the request
	log.Printf("Sending transcription request to whisper server (PID: %d): %s", s.cmd.Process.Pid, inferenceURL)
	s.debugLog(DebugTranscribe, "Sending request to whisper server: %s", inferenceURL)
	startTime := time.Now()

	// Add retry logic
	var resp *http.Response
	var respErr error

	for attempt := 0; attempt < s.maxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("Retrying transcription request (attempt %d/%d)", attempt+1, s.maxRetries)
			s.debugLog(DebugTranscribe, "Retrying request (attempt %d/%d)", attempt+1, s.maxRetries)
			time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
		}

		client := &http.Client{
			Timeout: 30 * time.Second,
		}
		resp, respErr = client.Do(req)

		if respErr == nil {
			break
		}
	}

	if respErr != nil {
		return nil, fmt.Errorf("failed to send request after %d attempts: %v", s.maxRetries, respErr)
	}
	defer resp.Body.Close()

	duration := time.Since(startTime)
	log.Printf("Received response from whisper server after %v", duration)
	s.debugLog(DebugTranscribe, "Received response from whisper server after %v", duration)

	// Check response status
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned error status %d: %s", resp.StatusCode, string(body))
	}

	// Read and parse the response
	var result WhisperServerResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		body, _ := io.ReadAll(resp.Body)
		s.debugLog(DebugTranscribe, "Failed to parse response: %v\nBody: %s", err, string(body))
		return nil, fmt.Errorf("failed to parse server response: %v", err)
	}

	result.Success = true
	s.debugLog(DebugTranscribe, "Transcription result: %s", result.Text)
	return &result, nil
}

// IsRunning returns true if the server is running
func (s *WhisperServerService) IsRunning() bool {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.isRunning
}

// Name returns the service name for shutdown management
func (s *WhisperServerService) Name() string {
	return "WhisperServer"
}

// Shutdown implements the Shutdownable interface
func (s *WhisperServerService) Shutdown() error {
	return s.Cleanup()
}

// Cleanup stops the whisper server and returns any error encountered
func (s *WhisperServerService) Cleanup() error {
	// First check if we need to do anything
	isRunning := s.IsRunning()
	if !isRunning {
		return nil
	}

	log.Println("Stopping whisper server")

	// Set running to false to prevent new operations
	s.mutex.Lock()
	s.isRunning = false
	cmd := s.cmd
	s.mutex.Unlock()

	// Try termination if we have a process
	if cmd != nil && cmd.Process != nil {
		pid := cmd.Process.Pid
		log.Printf("Terminating whisper server process (PID: %d)", pid)

		// First attempt with SIGTERM
		if err := cmd.Process.Signal(os.Interrupt); err != nil {
			log.Printf("Failed to send interrupt signal: %v", err)
		}

		// Wait with short timeout for graceful shutdown
		done := make(chan error, 1)
		go func() {
			done <- cmd.Wait()
		}()

		select {
		case <-done:
			log.Println("Whisper server process exited normally")
		case <-time.After(1 * time.Second):
			log.Println("Whisper server did not exit gracefully, force killing...")

			// Force kill with SIGKILL
			if err := cmd.Process.Kill(); err != nil {
				log.Printf("Failed to kill whisper server: %v", err)
			} else {
				log.Printf("Sent SIGKILL to whisper server process (PID: %d)", pid)
			}

			// Verify process is gone
			select {
			case <-done:
				log.Println("Whisper server process killed successfully")
			case <-time.After(1 * time.Second):
				// Use system kill as last resort
				log.Println("Process not responding to SIGKILL, using system kill...")
				killCmd := exec.Command("kill", "-9", fmt.Sprintf("%d", pid))
				if err := killCmd.Run(); err != nil {
					log.Printf("System kill failed: %v", err)
				} else {
					log.Printf("System kill sent to PID %d", pid)
				}
			}
		}
	} else {
		log.Println("No whisper server process to stop")
	}

	log.Println("Whisper server shutdown complete")
	return nil
}
