# ffaudio upstream provenance

- Upstream: https://github.com/stsaz/ffaudio.git
- Branch: `master`
- Synchronized commit: `6a951156e68e33d3925076e680ac56478a648c74`
- API version in `ffaudio/audio.h`: `26.01.31`

The files in `ffaudio/` are copied from the upstream commit without source
modifications. Upstream's root
`README.md` and `UNLICENSE` are also included in that directory. `UPSTREAM.md`
(this file) records the version used by the Go integration.

The Go/cgo wrapper (`ffaudio.go`, `ffaudio.c`, `ffaudio.h`, `utils.go`,
`ffaudio_windows.go`, `ffaudio_linux.go`, `pulse_devices_linux.c`, `pcm.go`, and `build.go`) is project-specific. It provides
device-name selection, microphone capture and speaker loopback modes, stream lifecycle,
PCM conversion, and safe buffer ownership.

The vendored `ffbase/` sources are synchronized with
[ffbase](https://github.com/stsaz/ffbase) commit
`2a7030bb4df67529f6180ae2f86b0a494df179ad` without source modifications. Its
upstream `UNLICENSE` is included in `ffbase/`. The integration uses these
sources on Windows/WASAPI and Linux/PulseAudio.
