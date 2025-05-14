package speech

import (
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestWhisperTranscription tests the transcription of the JFK sample
func TestWhisperTranscription(t *testing.T) {
	// Print process info
	t.Logf("Starting whisper test in process PID: %d", os.Getpid())

	// Skip test if JFK sample is not available
	samplePath := filepath.Join(os.Getenv("HOME"), "dev/whisper.cpp/samples/jfk.wav")
	if _, err := os.Stat(samplePath); os.IsNotExist(err) {
		t.Skip("Skipping test as JFK sample not found at", samplePath)
	}
	t.Logf("Found JFK sample at: %s", samplePath)

	// Read the WAV file
	t.Log("Reading WAV file...")
	audioData, err := readWavFile(samplePath)
	if err != nil {
		t.Fatalf("Failed to read WAV file: %v", err)
	}
	t.Logf("Successfully read WAV file, got %d samples at %d Hz", 
		len(audioData.Samples), audioData.SampleRate)

	// Create whisper service
	t.Log("Creating WhisperServerService...")
	whisperSvc := NewWhisperServerService()
	config := NewDefaultWhisperServerConfig()
	t.Logf("Using model: %s", config.ModelPath)
	t.Logf("Using server executable: %s", config.ServerPath)
	
	// Verify the server executable exists
	if _, err := os.Stat(config.ServerPath); os.IsNotExist(err) {
		t.Fatalf("Whisper server executable not found at: %s", config.ServerPath)
	}
	t.Log("Whisper server executable found")
	
	// Verify the model exists
	if _, err := os.Stat(config.ModelPath); os.IsNotExist(err) {
		t.Fatalf("Whisper model not found at: %s", config.ModelPath)
	}
	t.Log("Whisper model found")
	
	whisperSvc.WithConfig(config)

	// Initialize the server
	t.Log("Initializing whisper server...")
	initStart := time.Now()
	err = whisperSvc.Initialize()
	if err != nil {
		t.Fatalf("Failed to initialize whisper server: %v", err)
	}
	t.Logf("Whisper server initialized in %v", time.Since(initStart))
	
	defer func() {
		t.Log("Starting cleanup...")
		cleanupStart := time.Now()
		if err := whisperSvc.Cleanup(); err != nil {
			t.Logf("Error during cleanup: %v", err)
		}
		t.Logf("Cleanup completed in %v", time.Since(cleanupStart))
	}()

	// Transcribe the audio
	t.Log("Transcribing JFK sample...")
	startTime := time.Now()
	result, err := whisperSvc.Transcribe(audioData)
	transcriptionTime := time.Since(startTime)
	
	if err != nil {
		t.Fatalf("Failed to transcribe audio: %v", err)
	}

	// Expected text (partial match is fine)
	expectedText := "ask not what your country can do for you"
	
	t.Logf("Transcription completed in %v", transcriptionTime)
	t.Logf("Transcription result: %s", result.Text)
	
	if !strings.Contains(strings.ToLower(result.Text), strings.ToLower(expectedText)) {
		t.Errorf("Transcription did not contain expected text.\nExpected to contain: %s\nGot: %s", 
			expectedText, result.Text)
	}
}

// readWavFile reads a WAV file and returns AudioData
func readWavFile(filePath string) (*AudioData, error) {
	// Open the WAV file
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Skip the WAV header (44 bytes)
	if _, err := file.Seek(44, io.SeekStart); err != nil {
		return nil, err
	}

	// Read the audio data
	audioData := &AudioData{
		SampleRate: 16000, // Assume 16kHz sample rate
	}

	// Create a buffer to read the file
	const bufferSize = 1024
	buffer := make([]byte, bufferSize)
	samples := make([]int16, 0)

	for {
		// Read a chunk
		bytesRead, err := file.Read(buffer)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		// Convert bytes to int16 samples
		for i := 0; i < bytesRead; i += 2 {
			if i+1 < bytesRead {
				sample := int16(binary.LittleEndian.Uint16(buffer[i : i+2]))
				samples = append(samples, sample)
			}
		}
	}

	audioData.Samples = samples
	return audioData, nil
}