# Conch Development TODO List

> **Note:** Always run `gofmt -w .` before committing any code changes.

## Project Setup
- [x] Initialize Go project with modules
- [x] Add dependencies for SDL2, Bubbletea, and PTY libraries
- [x] Set up project directory structure
- [x] Ensure project builds with stub implementations

## Core Components
- [x] Develop SDL2 Audio Capture component
- [x] Build Whisper Transcription Service integration
- [ ] Move StatusService into pkgs. Refactor so it prints to a writer. in test_audio print to stdout.
- [ ] Implement PTY Manager for shell integration
- [ ] Create Input Management System with three modes
- [ ] Design Bubbletea TUI Layout
- [ ] Implement basic configuration system for settings

## Minimum Viable Product
- [ ] Integrate all core components into a working prototype
- [ ] Test end-to-end voice-to-command workflow
- [ ] Collect initial user feedback

## Features
- [ ] Implement mode switching keyboard shortcuts
- [ ] Add visual indicators for microphone status
- [ ] Create command verification for potentially destructive commands
- [ ] Implement transcription editing before execution
- [ ] Add voice command aliases
- [ ] Develop context-aware command suggestions

## Testing & Improvement
- [ ] Add robust error handling for audio capture failures
- [ ] Implement unit tests for core components
- [ ] Create component-specific test tools (e.g., SDL capture test)
- [ ] Implement graceful error recovery (auto-restart after crashes)
- [ ] Optimize SDL2+Whisper resource usage
- [ ] Test compatibility across different shell environments
- [ ] Add user configuration options
- [ ] Add performance benchmarks for transcription speed

## Documentation
- [ ] Document installation instructions
- [ ] Create usage guide with examples
- [ ] Add keyboard shortcut reference
