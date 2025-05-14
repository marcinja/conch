package terminal

import (
	"fmt"
	"strings"
	"sync"
	"time"
	"os/exec"
	"io"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/marcinja/conch/pkg/speech"
	"github.com/marcinja/conch/pkg/status"
)

// InputMode represents different input methods
type InputMode int

const (
	ManualMode InputMode = iota
	VoiceMode
)

// Custom message types
type errMsg struct {
	err error
}

type transcriptionMsg struct {
	text string
}

type statusUpdateMsg struct {
	text string
}

// TerminalApp manages the terminal UI for voice commands
type TerminalApp struct {
	program    *tea.Program
	model      *terminalModel
	speechSvc  *speech.SpeechService
	whisperSvc *speech.WhisperServerService
	statusSvc  *status.StatusService
}

// terminalModel implements the tea.Model interface
type terminalModel struct {
	// Services
	speechSvc      *speech.SpeechService
	whisperSvc     *speech.WhisperServerService
	statusSvc      *status.StatusService
	
	// UI state
	mode           InputMode
	statusMessage  string
	clipboardText  string
	transcriptions []string
	width          int
	height         int
	lastCtrlC      time.Time
	
	// UI styles
	styles         styles
	
	// Other
	mu             sync.Mutex
	program        *tea.Program
}

// styles holds the styling for the UI
type styles struct {
	statusBar      lipgloss.Style
	title          lipgloss.Style
	normalText     lipgloss.Style
	highlightText  lipgloss.Style
	dimText        lipgloss.Style
	errorText      lipgloss.Style
	focusedText    lipgloss.Style
	transcriptText lipgloss.Style
	historyText    lipgloss.Style
	historyTitle   lipgloss.Style
	currentTitle   lipgloss.Style
	clipboardTitle lipgloss.Style
	clipboardText  lipgloss.Style
	instructionText lipgloss.Style
	border         lipgloss.Style
	section        lipgloss.Style
	container      lipgloss.Style
}

// NewTerminalApp creates a new terminal application
func NewTerminalApp(shell string, speechSvc *speech.SpeechService, whisperSvc *speech.WhisperServerService, statusSvc *status.StatusService) (*TerminalApp, error) {
	// Create styles
	s := styles{
		statusBar:      lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#333333")).Padding(0, 1),
		title:          lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFCC00")).Align(lipgloss.Center).Padding(0, 4).MarginBottom(1),
		normalText:     lipgloss.NewStyle(),
		highlightText:  lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00FF00")),
		dimText:        lipgloss.NewStyle().Faint(true),
		errorText:      lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")),
		focusedText:    lipgloss.NewStyle().Bold(true),
		transcriptText: lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00")),
		historyText:    lipgloss.NewStyle().Faint(true),
		historyTitle:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFCC00")),
		currentTitle:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00CCFF")).Align(lipgloss.Center),
		clipboardTitle: lipgloss.NewStyle().Bold(true).Align(lipgloss.Center),
		clipboardText:  lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Align(lipgloss.Center),
		instructionText: lipgloss.NewStyle().Faint(true).Italic(true).Align(lipgloss.Center),
		border:         lipgloss.NewStyle().Border(lipgloss.DoubleBorder()).Padding(1, 3).BorderForeground(lipgloss.Color("#4B9CD3")),
		section:        lipgloss.NewStyle().Margin(1, 0),
		container:      lipgloss.NewStyle().Align(lipgloss.Center).Width(80),
	}

	// Initialize the model
	model := &terminalModel{
		speechSvc:      speechSvc,
		whisperSvc:     whisperSvc,
		statusSvc:      statusSvc,
		mode:           VoiceMode,
		statusMessage:  "Ready",
		clipboardText:  "",
		transcriptions: []string{},
		width:          80,
		height:         24,
		styles:         s,
	}

	// Create tea program
	program := tea.NewProgram(model, tea.WithAltScreen())
	app := &TerminalApp{
		program:    program,
		model:      model,
		speechSvc:  speechSvc,
		whisperSvc: whisperSvc,
		statusSvc:  statusSvc,
	}
	
	// Set the program reference in the model
	model.program = program

	return app, nil
}

// Run starts the terminal UI
func (app *TerminalApp) Run() error {
	// Start services if needed
	if app.statusSvc != nil {
		app.statusSvc.Start()
	}
	
	// Start the tea program - this will block until the program exits
	err := app.program.Start()
	
	return err
}

// --- Tea Model Implementation ---

// Init implements tea.Model
func (m *terminalModel) Init() tea.Cmd {
	return tea.Batch(
		checkForRecording(m.speechSvc, m.whisperSvc),
		checkStatus(m),
	)
}

// Update implements tea.Model
func (m *terminalModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Handle keyboard input
		switch msg.String() {
		case "ctrl+c":
			// Exit on Ctrl+C (double tap for confirmation)
			if time.Since(m.lastCtrlC) < time.Second {
				return m, tea.Quit
			}
			m.lastCtrlC = time.Now()
			m.statusMessage = "Press Ctrl+C again to exit"
			
		case "c", "C":
			// Clear clipboard text
			m.clipboardText = ""
			m.statusMessage = "Clipboard cleared"
			
		case "enter":
			// Copy text to clipboard
			if m.clipboardText != "" {
				err := copyToClipboard(m.clipboardText)
				if err != nil {
					m.statusMessage = fmt.Sprintf("Error copying to clipboard: %v", err)
				} else {
					m.statusMessage = "Copied to clipboard"
					// Add to transcriptions history
					if len(m.transcriptions) == 0 || m.transcriptions[len(m.transcriptions)-1] != m.clipboardText {
						m.transcriptions = append(m.transcriptions, m.clipboardText)
						// Keep only the last 5 transcriptions
						if len(m.transcriptions) > 5 {
							m.transcriptions = m.transcriptions[len(m.transcriptions)-5:]
						}
					}
				}
			}
		}
		
	case statusUpdateMsg:
		// Update status message
		m.statusMessage = msg.text
		return m, nil
		
	case transcriptionMsg:
		// Process the transcription
		text := strings.TrimSpace(msg.text)
		if text != "" {
			// Set as clipboard text
			m.clipboardText = text
			
			// Add to transcriptions if new
			if len(m.transcriptions) == 0 || m.transcriptions[len(m.transcriptions)-1] != text {
				m.transcriptions = append(m.transcriptions, text)
				// Keep only the last 5 transcriptions
				if len(m.transcriptions) > 5 {
					m.transcriptions = m.transcriptions[len(m.transcriptions)-5:]
				}
			}
		}
		
		// Continue checking for recordings
		cmds = append(cmds, checkForRecording(m.speechSvc, m.whisperSvc))
		
	case errMsg:
		m.statusMessage = "Error: " + msg.err.Error()
		// Continue checking for recordings
		cmds = append(cmds, checkForRecording(m.speechSvc, m.whisperSvc))
		
	case tea.WindowSizeMsg:
		// Update terminal size
		m.width = msg.Width
		m.height = msg.Height
	}

	// Always check status updates
	cmds = append(cmds, checkStatus(m))
	
	return m, tea.Batch(cmds...)
}

// View implements tea.Model
func (m *terminalModel) View() string {
	// Define layout
	var view strings.Builder

	// Adjust the container width based on terminal width
	containerWidth := m.width * 3 / 4 // 75% of terminal width
	if containerWidth < 60 {
		containerWidth = 60 // Minimum width
	}
	if containerWidth > 100 {
		containerWidth = 100 // Maximum width
	}
	m.styles.container = m.styles.container.Width(containerWidth)

	// Status bar at top - full width
	statusText := m.buildStatusText()
	statusBar := m.styles.statusBar.Width(m.width).Padding(1, 0).Render(statusText)
	view.WriteString(statusBar)
	view.WriteString("\n\n")
	
	// Main content: Transcription log (centered)
	logView := m.buildTranscriptionLog()
	centeredLog := m.styles.container.Render(logView)
	view.WriteString(centeredLog)
	view.WriteString("\n\n")
	
	// Current text area for clipboard (centered)
	clipboardView := m.buildClipboardView()
	centeredClipboard := m.styles.container.Render(clipboardView)
	view.WriteString(centeredClipboard)
	view.WriteString("\n\n")
	
	// Instructions at bottom (centered)
	instructions := "Press Enter to copy text to clipboard | Press 'c' to clear | Press Ctrl+C twice to exit"
	centeredInstructions := m.styles.container.Render(m.styles.instructionText.Render(instructions))
	view.WriteString(centeredInstructions)
	
	return view.String()
}

// buildStatusText creates the status bar text
func (m *terminalModel) buildStatusText() string {
	// Mode indicator
	modeText := "🎤 VOICE MODE"
	
	// Add speech service status indicators
	var statusIndicator string
	if m.speechSvc.IsRecording() {
		statusIndicator = "🔴 RECORDING"
	} else if m.speechSvc.IsTranscribing() {
		statusIndicator = "🔄 TRANSCRIBING"
	} else if m.speechSvc.IsListening() {
		statusIndicator = "🔊 LISTENING"
	} else {
		statusIndicator = "⏸️ IDLE"
	}
	
	// Combine everything
	return fmt.Sprintf("%s | %s | %s", modeText, statusIndicator, m.statusMessage)
}

// buildTranscriptionLog creates the transcription log view
func (m *terminalModel) buildTranscriptionLog() string {
	var log strings.Builder
	
	// Title - make bigger and centered
	log.WriteString(m.styles.title.Copy().Bold(true).Render("🐚 CONCH VOICE ASSISTANT 🐚"))
	log.WriteString("\n\n")
	
	// History section
	if len(m.transcriptions) > 1 {
		log.WriteString(m.styles.historyTitle.Render("📜 Recent History"))
		log.WriteString("\n\n")
		
		// All transcriptions except the most recent
		for i := 0; i < len(m.transcriptions)-1; i++ {
			log.WriteString(m.styles.historyText.Width(60).Render(m.transcriptions[i]))
			log.WriteString("\n\n") // Extra spacing
		}
	}
	
	// Latest transcription - with more emphasis
	if len(m.transcriptions) > 0 {
		log.WriteString(m.styles.currentTitle.Render("🔊 Latest Transcription"))
		log.WriteString("\n\n") // Extra space
		log.WriteString(m.styles.transcriptText.Bold(true).Width(60).Render(m.transcriptions[len(m.transcriptions)-1]))
		log.WriteString("\n")
	} else {
		// Show message when no transcriptions
		log.WriteString(m.styles.dimText.Render("Waiting for speech..."))
		log.WriteString("\n")
	}
	
	// Wrap in a border
	return m.styles.border.Render(log.String())
}

// buildClipboardView creates the clipboard view
func (m *terminalModel) buildClipboardView() string {
	var clipboard strings.Builder
	
	// Title with icons
	clipboard.WriteString(m.styles.clipboardTitle.Render("📋 Current Text"))
	clipboard.WriteString("\n\n")
	
	// Text - make it more prominent and wider
	if m.clipboardText != "" {
		clipboard.WriteString(m.styles.clipboardText.Width(60).Render(m.clipboardText))
	} else {
		clipboard.WriteString(m.styles.dimText.Render("No text to copy"))
	}
	clipboard.WriteString("\n\n")
	
	// Add key instructions inside
	instructions := "[Enter] Copy to clipboard | [C] Clear text"
	clipboard.WriteString(m.styles.dimText.Render(instructions))
	
	// Wrap in a border
	return m.styles.border.Render(clipboard.String())
}

// Helper functions

// copyToClipboard copies text to the system clipboard using pbcopy
func copyToClipboard(text string) error {
	cmd := exec.Command("pbcopy")
	
	// Connect stdin pipe
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("error creating stdin pipe: %w", err)
	}
	
	// Start the command
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("error starting pbcopy: %w", err)
	}
	
	// Write the text to stdin
	if _, err := io.WriteString(stdin, text); err != nil {
		return fmt.Errorf("error writing to stdin: %w", err)
	}
	
	// Close stdin to signal we're done
	if err := stdin.Close(); err != nil {
		return fmt.Errorf("error closing stdin: %w", err)
	}
	
	// Wait for the command to finish
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("error waiting for pbcopy: %w", err)
	}
	
	return nil
}

// checkForRecording checks for audio recording and transcribes it
func checkForRecording(speechSvc *speech.SpeechService, whisperSvc *speech.WhisperServerService) tea.Cmd {
	return func() tea.Msg {
		// Wait for audio recording with timeout
		audioData, err := speechSvc.WaitForRecording()
		if err != nil {
			if err.Error() == "service is shutting down" {
				return nil
			}
			return errMsg{err}
		}

		// Transcribe the audio
		result, err := whisperSvc.Transcribe(audioData)
		if err != nil {
			return errMsg{err}
		}

		// Clean up the text
		text := strings.TrimSpace(result.Text)
		return transcriptionMsg{text: text}
	}
}

// checkStatus periodically checks the status of services
func checkStatus(m *terminalModel) tea.Cmd {
	return tea.Tick(250*time.Millisecond, func(t time.Time) tea.Msg {
		var status string
		
		// Get status from speech service
		if m.speechSvc.IsRecording() {
			status = "Recording audio..."
		} else if m.speechSvc.IsTranscribing() {
			status = "Transcribing audio..."
		} else if m.speechSvc.IsListening() {
			status = "Listening for speech..."
		} else {
			status = "Ready"
		}
		
		return statusUpdateMsg{text: status}
	})
}
