
package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/gen2brain/malgo"
)

// ─── Config ──────────────────────────────────────────────────────────────────

const (
	sampleRate = 16000
	channels   = 1
	keyword    = "aura"

	// VAD — 30 ms frames (webrtcvad-compatible frame size)
	vadFrameMS      = 30
	vadFrameSamples = sampleRate * vadFrameMS / 1000 // 480 samples

	// How many consecutive silent frames = end of utterance (~800 ms)
	silenceFrames = 800 / vadFrameMS // 26 frames

	// How many voiced frames to confirm speech started (~150 ms)
	speechTrigger = 150 / vadFrameMS // 5 frames

	// Pre-roll: frames kept before speech trigger (~300 ms)
	preRollFrames = 300 / vadFrameMS // 10 frames

	// Energy VAD: voiced if RMS > threshold
	energyFloor = 300.0
	energyMult  = 2.8

	// Max utterance durations
	hotwordMaxSec = 5.0
	commandMaxSec = 60.0
)

// ─── GUI State ───────────────────────────────────────────────────────────────

var (
	statusLabel   *widget.Label
	transcriptLog *widget.Entry
	chatInput     *widget.Entry
	printMu       sync.Mutex
)

// setStatus updates the top label in the GUI
func setStatus(msg string) {
	printMu.Lock()
	fmt.Println("STATUS:", msg)
	if statusLabel != nil {
		// Proper thread-safe UI update using fyne.Do
		fyne.Do(func() {
			statusLabel.SetText(msg)
		})
	}
	printMu.Unlock()
}

// appendLog updates the transcript area in the GUI
func appendLog(speaker, msg string) {
	printMu.Lock()
	line := fmt.Sprintf("[%s] %s", strings.ToUpper(speaker), msg)
	fmt.Println(line)
	if transcriptLog != nil {
		// Proper thread-safe UI update using fyne.Do
		fyne.Do(func() {
			transcriptLog.SetText(transcriptLog.Text + line + "\n")
			transcriptLog.CursorRow = len(strings.Split(transcriptLog.Text, "\n")) - 1
		})
	}
	printMu.Unlock()
}

// speak uses Windows SAPI5 (via PowerShell) to speak text offline.
func speak(text string) {
	if text == "" {
		return
	}
	// Escape double quotes for PowerShell
	escaped := strings.ReplaceAll(text, "\"", "`\"")
	// Use SAPI5 (Microsoft Zira is usually the default female voice)
	psCommand := fmt.Sprintf("Add-Type -AssemblyName System.Speech; $synth = New-Object System.Speech.Synthesis.SpeechSynthesizer; $synth.SelectVoiceByHints([System.Speech.Synthesis.VoiceGender]::Female); $synth.Speak(\"%s\")", escaped)
	
	cmd := exec.Command("powershell", "-Command", psCommand)
	_ = cmd.Run()
}

// handleCommand checks for specific keywords and executes system tasks.
func handleCommand(text string) {
	text = strings.ToLower(text)

	// 1. Refresh Computer
	if strings.Contains(text, "refresh") {
		appendLog("AURA", "Refreshing your system...")
		go speak("Refreshing your system now.")
		// PowerShell trick to refresh desktop icons
		exec.Command("powershell", "-Command", "$shell = New-Object -ComObject Shell.Application; $shell.Namespace(0).Self.InvokeVerb('Refresh')").Run()
		return
	}

	// 2. Open Google / Smart Search
	if strings.Contains(text, "search") || strings.Contains(text, "google") {
		query := ""
		if strings.Contains(text, "search for") {
			parts := strings.SplitN(text, "search for", 2)
			query = strings.TrimSpace(parts[1])
		} else if strings.Contains(text, "search") {
			parts := strings.SplitN(text, "search", 2)
			query = strings.TrimSpace(parts[1])
		}
		
		if query != "" {
			appendLog("AURA", "Searching Google for: "+query)
			go speak("Searching Google for " + query)
			exec.Command("rundll32", "url.dll,FileProtocolHandler", "https://www.google.com/search?q="+query).Run()
		} else {
			appendLog("AURA", "Opening Google...")
			go speak("Opening Google.")
			exec.Command("rundll32", "url.dll,FileProtocolHandler", "https://www.google.com").Run()
		}
		return
	}

	// 3. Open Folder
	if strings.Contains(text, "open folder") || strings.Contains(text, "show folder") {
		target := "downloads"
		if strings.Contains(text, "folder") {
			parts := strings.SplitN(text, "folder", 2)
			target = strings.TrimSpace(parts[1])
		}
		
		home, _ := os.UserHomeDir()
		path := home + "\\" + target
		// Handle common aliases
		if target == "desktop" { path = "C:\\Users\\akatre\\OneDrive - Nice Software Solutions\\Desktop" }
		if target == "downloads" { path = home + "\\Downloads" }

		appendLog("AURA", "Opening folder: "+target)
		go speak("Opening " + target + " folder.")
		exec.Command("explorer", path).Run()
		return
	}

	// 4. Create Folder/Document
	if strings.Contains(text, "create") || strings.Contains(text, "make") {
		name := "New_Item"
		desktop := "C:\\Users\\akatre\\OneDrive - Nice Software Solutions\\Desktop\\"

		if strings.Contains(text, "folder") {
			parts := strings.SplitN(text, "folder", 2)
			name = strings.TrimSpace(parts[1])
			name = strings.TrimPrefix(name, "called ")
			os.MkdirAll(desktop+name, 0755)
			appendLog("AURA", "Created folder: "+name)
			go speak("Created folder " + name)
		} else if strings.Contains(text, "document") || strings.Contains(text, "file") {
			parts := strings.SplitN(text, "document", 2)
			if len(parts) < 2 { parts = strings.SplitN(text, "file", 2) }
			name = strings.TrimSpace(parts[1])
			name = strings.TrimPrefix(name, "called ")
			if !strings.Contains(name, ".") { name += ".txt" }
			
			os.WriteFile(desktop+name, []byte("Created by Aura Voice Assistant"), 0644)
			appendLog("AURA", "Created file: "+name)
			go speak("I created the document " + name)
		}
		return
	}

	// 5. Delete File
	if strings.Contains(text, "delete") || strings.Contains(text, "remove") {
		target := ""
		parts := strings.SplitN(text, "delete", 2)
		if len(parts) < 2 { parts = strings.SplitN(text, "remove", 2) }
		target = strings.TrimSpace(parts[1])
		target = strings.TrimPrefix(target, "file ")
		
		desktop := "C:\\Users\\akatre\\OneDrive - Nice Software Solutions\\Desktop\\"
		err := os.Remove(desktop + target)
		if err != nil {
			appendLog("SYSTEM", "Could not delete: "+target)
		} else {
			appendLog("AURA", "Deleted file: "+target)
			go speak("Deleted " + target)
		}
		return
	}

	// 6. Set Alarm/Timer
	if strings.Contains(text, "alarm") || strings.Contains(text, "timer") {
		// Simple parser for "alarm for 10 seconds"
		wait := 10 * time.Second
		if strings.Contains(text, "seconds") {
			// Extract number
			fmt.Sscanf(text, "set alarm for %d seconds", &wait)
			wait = wait * time.Second
		}
		
		appendLog("AURA", fmt.Sprintf("Setting alarm for %v", wait))
		go speak(fmt.Sprintf("Setting an alarm for %v", wait))
		go func() {
			time.Sleep(wait)
			appendLog("ALARM", "TIME IS UP!")
			speak("Wake up! Your timer is finished.")
		}()
		return
	}

	// 7. System Shutdown/Restart
	if strings.Contains(text, "shutdown") || strings.Contains(text, "turn off computer") {
		appendLog("AURA", "Shutting down computer in 60 seconds...")
		go speak("The computer will shut down in one minute. Save your work.")
		exec.Command("shutdown", "/s", "/t", "60").Run()
		return
	}
	if strings.Contains(text, "restart computer") {
		appendLog("AURA", "Restarting computer...")
		go speak("Restarting the computer now.")
		exec.Command("shutdown", "/r", "/t", "0").Run()
		return
	}
}

// ─── PCM helpers ─────────────────────────────────────────────────────────────

// rms computes root-mean-square of int16 PCM samples.
func rms(samples []int16) float64 {
	if len(samples) == 0 {
		return 0
	}
	var sum float64
	for _, s := range samples {
		v := float64(s)
		sum += v * v
	}
	return math.Sqrt(sum / float64(len(samples)))
}

// bytesToInt16 converts raw little-endian PCM bytes to []int16.
func bytesToInt16(b []byte) []int16 {
	out := make([]int16, len(b)/2)
	for i := range out {
		out[i] = int16(binary.LittleEndian.Uint16(b[i*2:]))
	}
	return out
}

// samplesToBytes converts []int16 back to raw PCM bytes.
func samplesToBytes(s []int16) []byte {
	b := make([]byte, len(s)*2)
	for i, v := range s {
		binary.LittleEndian.PutUint16(b[i*2:], uint16(v))
	}
	return b
}

// buildWAV wraps raw PCM int16 samples into a WAV byte slice.
func buildWAV(samples []int16) []byte {
	pcm := samplesToBytes(samples)
	var buf bytes.Buffer

	dataSize := uint32(len(pcm))
	chunkSize := 36 + dataSize

	buf.WriteString("RIFF")
	binary.Write(&buf, binary.LittleEndian, chunkSize)
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	binary.Write(&buf, binary.LittleEndian, uint32(16)) // subchunk1 size
	binary.Write(&buf, binary.LittleEndian, uint16(1))  // PCM
	binary.Write(&buf, binary.LittleEndian, uint16(channels))
	binary.Write(&buf, binary.LittleEndian, uint32(sampleRate))
	binary.Write(&buf, binary.LittleEndian, uint32(sampleRate*channels*2)) // byte rate
	binary.Write(&buf, binary.LittleEndian, uint16(channels*2))            // block align
	binary.Write(&buf, binary.LittleEndian, uint16(16))                    // bits per sample
	buf.WriteString("data")
	binary.Write(&buf, binary.LittleEndian, dataSize)
	buf.Write(pcm)

	return buf.Bytes()
}

// ─── VAD ─────────────────────────────────────────────────────────────────────

// VAD holds energy-based voice activity detection state.
// For higher accuracy install webrtcvad via CGO (see README).
type VAD struct {
	threshold   float64
	calibrated  bool
	calibBuf    []float64
	calibTarget int
}

func newVAD() *VAD {
	return &VAD{calibTarget: 30} // calibrate over ~30 frames = 0.9 s
}

// isSpeech returns true if the frame contains voiced audio.
// First calibTarget frames are used for ambient calibration.
func (v *VAD) isSpeech(frame []int16) bool {
	energy := rms(frame)
	if !v.calibrated {
		v.calibBuf = append(v.calibBuf, energy)
		if len(v.calibBuf) >= v.calibTarget {
			// Use median of calibration frames as ambient baseline
			median := medianFloat(v.calibBuf)
			v.threshold = math.Max(median*energyMult, energyFloor)
			v.calibrated = true
			setStatus(fmt.Sprintf("VAD Calibrated — threshold: %.0f", v.threshold))
		}
		return false
	}
	return energy > v.threshold
}

func (v *VAD) ready() bool { return v.calibrated }

func medianFloat(data []float64) float64 {
	cp := make([]float64, len(data))
	copy(cp, data)
	// Simple insertion sort (small slice)
	for i := 1; i < len(cp); i++ {
		for j := i; j > 0 && cp[j] < cp[j-1]; j-- {
			cp[j], cp[j-1] = cp[j-1], cp[j]
		}
	}
	return cp[len(cp)/2]
}

// ─── Audio capture ───────────────────────────────────────────────────────────

// AudioCapture streams 30 ms PCM frames via a channel using malgo (miniaudio).
// malgo is a pure-Go binding — no PortAudio, no MSVC, no system libs on Windows.
type AudioCapture struct {
	frames chan []int16
	ctx    *malgo.AllocatedContext
	device *malgo.Device
}

func newAudioCapture() (*AudioCapture, error) {
	ac := &AudioCapture{
		frames: make(chan []int16, 512),
	}

	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, fmt.Errorf("malgo context: %w", err)
	}
	ac.ctx = ctx

	deviceConfig := malgo.DefaultDeviceConfig(malgo.Capture)
	deviceConfig.Capture.Format = malgo.FormatS16
	deviceConfig.Capture.Channels = channels
	deviceConfig.SampleRate = sampleRate
	deviceConfig.PeriodSizeInFrames = vadFrameSamples // 30 ms per callback

	callbacks := malgo.DeviceCallbacks{
		Data: func(outputSamples, inputSamples []byte, frameCount uint32) {
			frame := bytesToInt16(inputSamples)
			// Non-blocking send — drop frame if consumer is behind
			select {
			case ac.frames <- frame:
			default:
			}
		},
	}

	device, err := malgo.InitDevice(ctx.Context, deviceConfig, callbacks)
	if err != nil {
		ctx.Uninit()
		return nil, fmt.Errorf("malgo device: %w", err)
	}
	ac.device = device
	return ac, nil
}

func (ac *AudioCapture) Start() error { return ac.device.Start() }
func (ac *AudioCapture) Stop()        { ac.device.Stop(); ac.ctx.Uninit() }

// Drain discards buffered frames (call before command recording).
func (ac *AudioCapture) Drain() {
	for {
		select {
		case <-ac.frames:
		default:
			return
		}
	}
}

// ─── Utterance collector ─────────────────────────────────────────────────────

// collectUtterance blocks until a complete utterance is captured via VAD.
// Returns nil if ctx is cancelled before speech starts or nothing is found.
func collectUtterance(ctx context.Context, ac *AudioCapture, vad *VAD, maxSec float64) []int16 {
	preRoll := make([][]int16, 0, preRollFrames)
	silenceBuf := make([]bool, 0, silenceFrames)
	voicedRun := 0
	recording := false
	collected := []int16{}
	maxFrames := int(maxSec * 1000 / vadFrameMS)
	n := 0

	for n < maxFrames {
		select {
		case <-ctx.Done():
			return nil
		case frame, ok := <-ac.frames:
			if !ok {
				return nil
			}
			n++
			voiced := vad.isSpeech(frame)

			if !recording {
				// Ring pre-roll buffer
				if len(preRoll) >= preRollFrames {
					preRoll = preRoll[1:]
				}
				preRoll = append(preRoll, frame)

				if voiced {
					voicedRun++
					if voicedRun >= speechTrigger {
						// Speech confirmed — start collecting
						recording = true
						for _, f := range preRoll {
							collected = append(collected, f...)
						}
						silenceBuf = silenceBuf[:0]
					}
				} else {
					voicedRun = 0
				}
			} else {
				collected = append(collected, frame...)

				// Maintain silence ring buffer
				if len(silenceBuf) >= silenceFrames {
					silenceBuf = silenceBuf[1:]
				}
				silenceBuf = append(silenceBuf, voiced)

				// End of utterance: full buffer of silence
				if len(silenceBuf) == silenceFrames && allFalse(silenceBuf) {
					break
				}
			}
		}
	}

	minSamples := speechTrigger * 2 * vadFrameSamples
	if len(collected) < minSamples {
		return nil
	}
	return collected
}

func allFalse(b []bool) bool {
	for _, v := range b {
		if v {
			return false
		}
	}
	return true
}

// ─── Transcription ───────────────────────────────────────────────────────────

// transcribeResponse matches the undocumented free Chromium STT API
type transcribeResponse struct {
	Result []struct {
		Alternative []struct {
			Transcript string  `json:"transcript"`
			Confidence float32 `json:"confidence"`
		} `json:"alternative"`
	} `json:"result"`
}

// transcribe sends audio to Google's free Speech-to-Text and returns the transcript.
// It bypasses the paid API by using the Chromium undocumented endpoint.
func transcribe(samples []int16) (string, error) {
	// The undocumented API requires raw L16 PCM audio
	pcm := samplesToBytes(samples)

	url := "http://www.google.com/speech-api/v2/recognize?client=chromium&lang=en-US&key=AIzaSyBOti4mM-6x9WDnZIjIeyEU21OpBXqWBgw&pFilter=0"
	req, err := http.NewRequest("POST", url, bytes.NewReader(pcm))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "audio/l16; rate=16000")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API error %d: %s", resp.StatusCode, string(b))
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// Response is multiple newline-separated JSON objects
	lines := strings.Split(string(b), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var result transcribeResponse
		if err := json.Unmarshal([]byte(line), &result); err != nil {
			continue
		}
		if len(result.Result) > 0 && len(result.Result[0].Alternative) > 0 {
			transcript := result.Result[0].Alternative[0].Transcript
			return strings.ToLower(transcript), nil
		}
	}

	return "", nil
}

// transcribeAsync runs transcription in a goroutine and sends result to ch.
func transcribeAsync(samples []int16, ch chan<- string) {
	go func() {
		text, err := transcribe(samples)
		if err != nil {
			if strings.Contains(err.Error(), "network") {
				ch <- "__NET_ERR__"
			} else {
				ch <- "__API_ERR__:" + err.Error()
			}
			return
		}
		ch <- text
	}()
}

// ─── State machine ────────────────────────────────────────────────────────────

func run(ac *AudioCapture, vad *VAD) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Wait for VAD calibration
	if !vad.ready() {
		setStatus("Calibrating ambient noise — stay quiet for 1 second...")
		for !vad.ready() {
			select {
			case <-ctx.Done():
				return
			case frame := <-ac.frames:
				vad.isSpeech(frame) // feeds calibration
			}
		}
	}

	resultCh := make(chan string, 2)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// ── PHASE 1: hotword scan ──────────────────────────────────────
		setStatus(fmt.Sprintf("Sleeping — Waiting for '%s'...", strings.ToUpper(keyword)))

		hwSamples := collectUtterance(ctx, ac, vad, hotwordMaxSec)
		if hwSamples == nil {
			continue
		}

		// Submit hotword transcription to its own goroutine (non-blocking)
		transcribeAsync(hwSamples, resultCh)

		var hwText string
		select {
		case <-ctx.Done():
			return
		case hwText = <-resultCh:
		case <-time.After(7 * time.Second):
			continue // timed out
		}

		switch {
		case hwText == "__NET_ERR__":
			appendLog("SYSTEM", "Network error — check your connection.")
			continue
		case strings.HasPrefix(hwText, "__API_ERR__"):
			appendLog("SYSTEM", "API Error: "+hwText)
			continue
		case !strings.Contains(hwText, keyword):
			continue // keyword not found, keep scanning
		}

		// ── PHASE 2: wake confirmed ────────────────────────────────────
		appendLog("WAKE", fmt.Sprintf("Heard: \"%s\"", hwText))

		// Check for inline command ("Aura set a timer for 5 minutes")
		parts := strings.SplitN(hwText, keyword, 2)
		inline := strings.TrimSpace(parts[len(parts)-1])

		if inline != "" {
			appendLog("YOU", inline)
			continue
		}

		// ── PHASE 3: listen for full command (unlimited length) ────────
		setStatus("Listening... speak freely, pause when done.")
		ac.Drain() // discard stale audio from before wake

		cmdSamples := collectUtterance(ctx, ac, vad, commandMaxSec)
		if cmdSamples == nil {
			appendLog("SYSTEM", "Nothing detected — say 'Aura' again")
			continue
		}

		dur := float64(len(cmdSamples)) / sampleRate
		setStatus(fmt.Sprintf("Transcribing %.1fs of audio...", dur))

		transcribeAsync(cmdSamples, resultCh)

		var cmdText string
		select {
		case <-ctx.Done():
			return
		case cmdText = <-resultCh:
		case <-time.After(20 * time.Second):
			appendLog("SYSTEM", "Transcription timed out")
			continue
		}

		switch {
		case cmdText == "__NET_ERR__":
			appendLog("SYSTEM", "Network error during transcription.")
		case strings.HasPrefix(cmdText, "__API_ERR__"):
			appendLog("SYSTEM", "API Error: "+cmdText)
		case cmdText == "":
			appendLog("SYSTEM", "Could not understand — please try again")
		default:
			appendLog("YOU", cmdText)
			handleCommand(cmdText)
		}
	}
}

// ─── Entry point ──────────────────────────────────────────────────────────────

func main() {
	a := app.New()
	w := a.NewWindow("Aura Voice Assistant")

	statusLabel = widget.NewLabel("Initializing...")
	statusLabel.Alignment = fyne.TextAlignCenter
	statusLabel.TextStyle.Bold = true

	transcriptLog = widget.NewMultiLineEntry()
	transcriptLog.Disable() // read-only
	transcriptLog.SetPlaceHolder("Your conversation will appear here...")

	scroll := container.NewScroll(transcriptLog)

	// Create audio capture
	ac, err := newAudioCapture()
	if err != nil {
		statusLabel.SetText("FATAL: Audio init failed")
		appendLog("SYSTEM", fmt.Sprintf("Error connecting to mic: %v", err))
		w.ShowAndRun()
		return
	}
	defer ac.Stop()

	if err := ac.Start(); err != nil {
		statusLabel.SetText("FATAL: Mic start failed")
		appendLog("SYSTEM", fmt.Sprintf("Could not start microphone: %v", err))
		w.ShowAndRun()
		return
	}

	vad := newVAD()

	// ─── Chat Input Handler ──────────────────────────────────────────────
	chatInput = widget.NewEntry()
	chatInput.SetPlaceHolder("Type here and press Enter to make Aura speak...")
	chatInput.OnSubmitted = func(text string) {
		if strings.TrimSpace(text) == "" {
			return
		}
		appendLog("YOU (Typed)", text)
		chatInput.SetText("") // Clear input
		handleCommand(text)   // Process command
	}

	// Start Aura's main loop in background
	go run(ac, vad)

	// Layout
	content := container.NewBorder(
		container.NewPadded(statusLabel),
		container.NewPadded(chatInput), // Input at the bottom
		nil, nil,
		scroll,
	)

	w.SetContent(content)
	w.Resize(fyne.NewSize(600, 500))
	w.ShowAndRun()
}
