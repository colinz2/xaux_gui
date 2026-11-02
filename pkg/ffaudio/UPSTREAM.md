# ffaudio upstream provenance

- Upstream: https://github.com/stsaz/ffaudio.git
- Branch: `master`
- Synchronized commit: `6a951156e68e33d3925076e680ac56478a648c74`
- API version in `ffaudio/audio.h`: `26.01.31`

The files in `ffaudio/` are copied from the upstream commit without source
modifications. Upstream's root `README.md` and `UNLICENSE` are also included in
that directory. `UPSTREAM.md` (this file) records the version used by the Go
integration.

The Go/cgo wrapper (`ffaudio.go`, `ffaudio.c`, `ffaudio.h`, `utils.go`,
`ffaudio_windows.go`, `pcm.go`, and `build.go`) is project-specific. It provides
device-name selection, capture and WASAPI loopback modes, stream lifecycle,
PCM conversion, and safe buffer ownership.

The existing vendored `ffbase/` headers were retained. The synchronized ffaudio
sources compile and pass the Windows/WASAPI tests with those headers; ffbase was
not independently upgraded as part of this change.
