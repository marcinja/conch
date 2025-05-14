package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/marcinja/conch/pkg/common"
	"github.com/marcinja/conch/pkg/speech"
	"github.com/marcinja/conch/pkg/status"
)

// WAV file header structure
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

// writeWavFile writes audio data to a WAV file
func writeWavFile(filename string, samples []int16, sampleRate int) error {
	// Create file
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

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
		return err
	}

	// Write audio data
	if err := binary.Write(file, binary.LittleEndian, samples); err != nil {
		return err
	}

	return nil
}

// StatusService replaced by pkg/status/status.go

func main() {
	log.SetPrefix("audio-test: ")
	log.SetFlags(log.Ltime)

	log.Println("Starting audio capture test")

	// Create speech service
	svc := speech.NewSpeechService()

	// Create whisper service - initialize it in advance
	whisperSvc := speech.NewWhisperServerService()

	// Create status service
	statusSvc := status.NewStatusService(svc)

	// Set up graceful shutdown handler
	shutdownManager := common.NewGracefulShutdown(10 * time.Second)
	shutdownManager.Register(statusSvc)  // Register status service
	shutdownManager.Register(whisperSvc) // Register whisper service
	shutdownManager.Register(svc)        // Register speech service last
	shutdownManager.Start()

	// Initialize services
	if err := whisperSvc.Initialize(); err != nil {
		log.Fatalf("Failed to initialize whisper service: %v", err)
	}

	if err := svc.Initialize(); err != nil {
		log.Fatalf("Failed to initialize speech service: %v", err)
	}

	// Start listening
	if err := svc.StartListening(); err != nil {
		log.Fatalf("Failed to start listening: %v", err)
	}

	fmt.Println("Listening for speech. Speak now, or press Ctrl+C to exit.")
	fmt.Println("Status:")

	// Start status display
	statusSvc.Start()

	recordingCount := 0

	// Main loop
	for {
		// Wait for audio recording with timeout
		audioData, err := svc.WaitForRecording()
		if err != nil {
			if err.Error() == "service is shutting down" {
				log.Println("Shutting down recording loop")
				return
			}
			log.Printf("Error waiting for recording: %v", err)
			continue
		}

		// Increment recording counter
		recordingCount++

		// Save to WAV file
		filename := fmt.Sprintf("recording_%d.wav", recordingCount)
		if err := writeWavFile(filename, audioData.Samples, audioData.SampleRate); err != nil {
			log.Printf("Error saving WAV file: %v", err)
		} else {
			fmt.Printf("\rSaved recording to %s\n", filename)
		}

		// Got a recording, transcribe it using WhisperServerService directly
		fmt.Print("\rStarting transcription... 🔄 ")

		// Transcribe the audio
		result, err := whisperSvc.Transcribe(audioData)

		if err != nil {
			log.Printf("Error transcribing: %v", err)
			continue
		}

		fmt.Printf("\rTranscription: %s\n", result.Text)
		fmt.Println("Listening again. Speak now, or press Ctrl+C to exit.")
		fmt.Print("Status: ")
	}
}
