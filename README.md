# Conch: Voice-Controlled Terminal Interface

Conch is a voice-controlled terminal interface that lets users seamlessly switch between typing and speaking to interact with their shell. The application uses SDL2 for real-time audio capture, whisper.cpp for accurate speech-to-text transcription, and Bubbletea for a responsive terminal UI.
Key features:

Toggle between voice input and manual typing with keyboard shortcuts
Real-time voice activity detection and transcription
Direct integration with your existing shell
Visual feedback on voice recognition status
Preview transcribed commands before execution
Full terminal output display within the TUI

This tool enhances productivity by allowing hands-free operation of command-line interfaces, making terminal usage more accessible and efficient. Perfect for developers who want to dictate commands while coding, system administrators managing servers, or anyone seeking a more natural way to interact with their terminal.

# Detailed Design: Voice-Controlled TUI Shell with Bubbletea and SDL2

Here's a comprehensive design for your application combining SDL2 for audio capture, whisper.cpp for transcription, and bubbletea for the TUI with shell integration:

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│                       Bubbletea TUI                         │
├───────────────┬───────────────────────────┬─────────────────┤
│ Mode Controls │     Shell Output Area     │ Status Display  │
└───────┬───────┴───────────────┬───────────┴────────┬────────┘
        │                       │                    │
        ▼                       │                    ▼
┌───────────────┐              │             ┌─────────────────┐
│ Input Manager │              │             │ Status Manager  │
└───────┬───────┘              │             └─────────────────┘
        │                      │
┌───────┴───────┐             │
│  SDL2 Audio   │◄────────────┘
│   Capture     │              │
└───────┬───────┘              │
        │                      │
        ▼                      ▼
┌───────────────┐     ┌─────────────────┐
│   Whisper     │     │  Pseudo-Terminal│
│ Transcription │────▶│  (PTY) Manager  │
└───────────────┘     └─────────────────┘
```

## Key Components

### 1. PTY (Pseudo-Terminal) Manager
```go
import "github.com/creack/pty"
```

- Creates a bidirectional pseudo-terminal for running your shell
- Connects to the specified shell or command
- Manages stdin/stdout/stderr streams
- Provides methods to:
  - Write input (from voice or keyboard)
  - Read output (to display in TUI)
  - Handle terminal signals

### 2. Input Management System
- Manages three distinct input modes:
  1. **Voice Mode**: Captures and transcribes speech
  2. **Manual Mode**: Direct keyboard input to shell
  3. **Command Mode**: Special TUI commands (not sent to shell)
- Handles mode toggling via keyboard shortcuts

### 3. SDL2 Audio Capture
```go
import "github.com/veandco/go-sdl2/sdl"
```

- Continuously monitors audio input in a separate goroutine
- Implements Voice Activity Detection (VAD) to detect speech
- Buffers audio when speech is detected
- Signals when speech has ended

### 4. Whisper Transcription Service
- Invokes whisper.cpp binary with captured audio
- Processes transcription results
- Optionally formats output (commands vs. text)
- Can be configured with different models

### 5. Bubbletea TUI Layout
```go
import "github.com/charmbracelet/bubbletea"
import "github.com/charmbracelet/lipgloss"
```

- **Shell Output Area**: Main portion showing terminal output
- **Input Preview**: Shows transcribed text before sending
- **Status Bar**: Displays current mode and system status
- **Mode Indicator**: Visual indicator of current input mode

## Interaction Flow

1. **Initialization**:
   - Start bubbletea application
   - Initialize SDL2 audio capture
   - Create PTY and start target shell

2. **Mode Switching**:
   - `Alt+V`: Toggle voice mode
   - `Alt+M`: Switch to manual typing mode
   - `Alt+C`: Switch to command mode (for TUI controls)

3. **Voice Input Process**:
   ```
   ┌───────────┐    ┌──────────┐    ┌───────────┐    ┌─────────┐    ┌──────────┐
   │ Detect    │───▶│ Buffer   │───▶│ Transcribe│───▶│ Preview │───▶│ Send to  │
   │ Speech    │    │ Audio    │    │ with      │    │ in TUI  │    │ Shell    │
   └───────────┘    └──────────┘    │ Whisper   │    └─────────┘    └──────────┘
                                   └───────────┘
   ```

4. **Manual Input Process**:
   - Direct keyboard input to shell
   - Standard terminal controls (Ctrl+C, etc.)

5. **Visual Indicators**:
   - Microphone icon changes color based on status:
     - Gray: Voice mode inactive
     - Yellow: Listening (no speech detected)
     - Red: Recording speech
     - Blue: Transcribing
     - Green: Transcription complete

## Implementation Details

### PTY Setup
```go
func createPTY(shellCmd string) (*os.File, *exec.Cmd, error) {
    c := exec.Command(shellCmd)
    ptmx, err := pty.Start(c)
    if err != nil {
        return nil, nil, err
    }
    return ptmx, c, nil
}
```

### Voice Detection with SDL2
```go
func startAudioCapture(audioChannel chan []int16) {
    // Initialize SDL2
    sdl.Init(sdl.INIT_AUDIO)
    defer sdl.Quit()
    
    // Configure audio capture
    spec := sdl.AudioSpec{
        Freq:     16000,
        Format:   sdl.AUDIO_S16,
        Channels: 1,
        Samples:  1024,
        Callback: sdl.AudioCallback(captureCallback),
    }
    
    // Open audio device and start capturing
    // ...
}
```

### Bubbletea Model Structure
```go
type model struct {
    pty           *os.File
    shellCmd      *exec.Cmd
    shellOutput   string
    inputPreview  string
    mode          int // 0=manual, 1=voice, 2=command
    isListening   bool
    isRecording   bool
    isTranscribing bool
    statusMessage string
    // ...
}
```

## Advanced Features

1. **Command Verification**:
   - When transcribing potential shell commands, show a confirmation dialog
   - Especially useful for potentially destructive commands

2. **Transcription Editing**:
   - Allow editing the transcribed text before sending to shell
   - Quick correction shortcuts

3. **Command Aliases**:
   - Define voice shortcuts for common commands
   - E.g., saying "list files" executes "ls -la"

4. **Context-Aware Commands**:
   - Recognize context in the current shell
   - Suggest completions based on current directory contents

