package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/marcinja/conch/pkg/common"
	"github.com/marcinja/conch/pkg/speech"
	"github.com/marcinja/conch/pkg/status"
	"github.com/marcinja/conch/pkg/terminal" // Using bubbletea
	// "github.com/marcinja/conch/pkg/terminal_tview" // Using tview
)

func main() {
	log.SetPrefix("conch: ")
	log.SetFlags(log.Ltime)

	log.Println("Starting conch terminal")

	// Initialize configuration
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash" // Default shell if not set
	}

	// Create services
	speechSvc := speech.NewSpeechService()
	whisperSvc := speech.NewWhisperServerService()
	
	// Status service will be passed to the terminal app
	statusSvc := status.NewStatusService(speechSvc)

	// Set up graceful shutdown handler
	shutdownManager := common.NewGracefulShutdown(10 * time.Second)
	shutdownManager.Register(statusSvc)  // Register status service
	shutdownManager.Register(whisperSvc) // Register whisper service
	shutdownManager.Register(speechSvc)  // Register speech service last
	shutdownManager.Start()

	// Initialize services
	if err := whisperSvc.Initialize(); err != nil {
		log.Fatalf("Failed to initialize whisper service: %v", err)
	}

	if err := speechSvc.Initialize(); err != nil {
		log.Fatalf("Failed to initialize speech service: %v", err)
	}

	// Start listening
	if err := speechSvc.StartListening(); err != nil {
		log.Fatalf("Failed to start listening: %v", err)
	}

	// Register the shutdown handler before running the terminal app
	shutdownChan := make(chan os.Signal, 1)
	shutdownManager.SetSignalHandler(shutdownChan)
	
	// Create and run terminal app with bubbletea
	app, err := terminal.NewTerminalApp(shell, speechSvc, whisperSvc, statusSvc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing application: %v\n", err)
		os.Exit(1)
	}

	log.Println("Starting terminal UI")
	if err := app.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running application: %v\n", err)
	}
	
	// Trigger graceful shutdown when app terminates
	log.Println("Terminal UI exited, shutting down services")
	shutdownManager.StartShutdown()
}
