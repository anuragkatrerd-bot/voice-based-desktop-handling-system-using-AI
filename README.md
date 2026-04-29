# voice-based-desktop-handling-system-using-AI

## AURA Voice Assistant — Go Edition

Single `.exe`, zero runtime, no pip, no MSVC, no wheel errors.

## Why Go instead of Python?

| Problem in Python         | Go solution                          |
|---------------------------|--------------------------------------|
| webrtcvad needs MSVC      | malgo = pure Go, zero C compiler     |
| pip dependency hell       | `go get` resolves everything         |
| GIL limits true multicore | Goroutines = real OS threads         |
| Must install Python first | Single `.exe`, ships standalone      |
| sounddevice wheel issues  | malgo wraps miniaudio, pre-compiled  |

---

## Step 1 — Install Go (one time only)

Download from https://go.dev/dl/ — grab the Windows `.msi` installer.  
After install, open a **new** Command Prompt and verify:

```
go version
```

You should see something like `go version go1.22.0 windows/amd64`.

---

## Step 2 — Get a Google Speech API key (free tier is generous)

1. Go to https://console.cloud.google.com/
2. Create a project (or use an existing one)
3. Search for **"Cloud Speech-to-Text API"** → Enable it
4. Go to **APIs & Services → Credentials → Create Credentials → API Key**
5. Copy the key

The free tier gives you **60 minutes of audio per month** at no cost.

---

## Step 3 — Set your API key

In Command Prompt (do this once per session, or add to your system env vars):

```cmd
set GOOGLE_API_KEY=your_key_here
```

To make it permanent (so you don't have to set it every time):
1. Press Win+R → type `sysdm.cpl` → Advanced → Environment Variables
2. Under "User variables" → New → Name: `GOOGLE_API_KEY`, Value: your key

---

## Step 4 — Build Aura

Open Command Prompt in the folder containing these files, then:

```cmd
go mod tidy
go build -o aura.exe .
```

`go mod tidy` downloads the one dependency (malgo ~200 KB).  
`go build` compiles everything into a single `aura.exe`.

That's it. No pip. No MSVC. No wheels.

---

## Step 5 — Run

```cmd
aura.exe
```

Or double-click `aura.exe` from Explorer.

### First run:
- Aura stays silent for ~1 second to calibrate ambient noise (keep quiet)
- Then shows: `[SLEEP]  Waiting for 'AURA'...`
- Say **"Aura"** to wake it
- Speak your command — it stops recording when you pause

### Examples:
```
You: "Aura what's the weather today"
     → captured in one breath, transcribed instantly

You: "Aura"
     → activates command mode
You: "Set a timer for ten minutes and also remind me to check my email"
     → records the full sentence however long it is, stops when you pause
```

---

## Tuning (edit main.go)

| Constant        | Default | What it does                                   |
|-----------------|---------|------------------------------------------------|
| `silenceFrames` | 26      | Silence duration before stopping (800 ms)      |
| `speechTrigger` | 5       | Voiced frames to confirm speech (150 ms)       |
| `energyMult`    | 2.8     | VAD sensitivity (lower = more sensitive)       |
| `energyFloor`   | 300     | Minimum noise threshold (raise in loud rooms)  |
| `commandMaxSec` | 60      | Maximum recording length per command           |
| `keyword`       | "aura"  | Wake word (change to anything)                 |

After editing: `go build -o aura.exe .` — compiles in ~2 seconds.

---

## Distributing to another PC

Just copy `aura.exe`. No Go installation needed on the target machine.  
The other person only needs to set their own `GOOGLE_API_KEY`.

---

## Troubleshooting

**"Audio init failed"**  
→ Make sure a microphone is connected and set as default in Windows sound settings.

**"API error 403"**  
→ Your API key is wrong or the Speech-to-Text API isn't enabled in your Google project.

**"API error 400"**  
→ Usually means an empty audio clip. Try speaking louder or adjusting `energyFloor`.

**Commands getting cut off**  
→ Increase `silenceFrames` in main.go (e.g. change `800/vadFrameMS` to `1200/vadFrameMS`)  
→ This gives 1.2 seconds of silence before stopping instead of 0.8 s.

**VAD triggering on background noise**  
→ Increase `energyMult` from 2.8 to 3.5 or 4.0 in main.go.

**Coloured output not showing in old Command Prompt**  
→ Use Windows Terminal (free from Microsoft Store) — it supports ANSI natively.
