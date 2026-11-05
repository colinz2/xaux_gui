//go:build tenvad && windows && cgo

package vad

/*
#include <windows.h>
#include <stdint.h>
#include <stdlib.h>

typedef int (*vad_create_fn)(void **, size_t, float);
typedef int (*vad_process_fn)(void *, const int16_t *, size_t, float *, int *);
typedef int (*vad_destroy_fn)(void **);
typedef struct {
	HMODULE library;
	void *handle;
	vad_process_fn process;
	vad_destroy_fn destroy;
} vad_instance;

static int vad_open(const wchar_t *path, vad_instance **out) {
	vad_instance *v = calloc(1, sizeof(*v));
	if (!v) return -1;
	v->library = LoadLibraryExW(path, NULL, LOAD_WITH_ALTERED_SEARCH_PATH);
	if (!v->library) { free(v); return -2; }
	vad_create_fn create = (vad_create_fn)GetProcAddress(v->library, "ten_vad_create");
	v->process = (vad_process_fn)GetProcAddress(v->library, "ten_vad_process");
	v->destroy = (vad_destroy_fn)GetProcAddress(v->library, "ten_vad_destroy");
	if (!create || !v->process || !v->destroy) {
		FreeLibrary(v->library); free(v); return -3;
	}
	if (create(&v->handle, 256, 0.5f) != 0) {
		FreeLibrary(v->library); free(v); return -4;
	}
	*out = v;
	return 0;
}
static int vad_process(vad_instance *v, const int16_t *samples, int *flag) {
	float probability;
	return v->process(v->handle, samples, 256, &probability, flag);
}
static int vad_close(vad_instance *v) {
	int result = v->destroy(&v->handle);
	FreeLibrary(v->library);
	free(v);
	return result;
}
*/
import "C"

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

type nativeDetector struct {
	instance *C.vad_instance
}

func New() (Detector, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("locate TEN VAD DLL: %w", err)
	}
	path := filepath.Join(filepath.Dir(exe), "ten_vad.dll")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// Development/test binaries live outside the source tree.
		_, source, _, ok := runtime.Caller(0)
		if !ok {
			return nil, fmt.Errorf("locate TEN VAD development library")
		}
		arch := "x64"
		if runtime.GOARCH == "386" {
			arch = "x86"
		} else if runtime.GOARCH != "amd64" {
			return nil, fmt.Errorf("TEN VAD Windows architecture %s is unsupported", runtime.GOARCH)
		}
		path = filepath.Join(filepath.Dir(source), "ten-vad", "lib", "Windows", arch, "ten_vad.dll")
	}
	widePath, err := windows.UTF16FromString(path)
	if err != nil {
		return nil, fmt.Errorf("TEN VAD DLL path: %w", err)
	}
	d := &nativeDetector{}
	if code := C.vad_open((*C.wchar_t)(unsafe.Pointer(&widePath[0])), &d.instance); code != 0 {
		return nil, fmt.Errorf("load TEN VAD %q: code %d (check DLL architecture and MSVC runtime)", path, int(code))
	}
	return d, nil
}

func (d *nativeDetector) Process(samples []int16) (bool, error) {
	if d.instance == nil || len(samples) != FrameSamples {
		return false, fmt.Errorf("TEN VAD requires an open detector and %d samples", FrameSamples)
	}
	var flag C.int
	if code := C.vad_process(d.instance, (*C.int16_t)(unsafe.Pointer(&samples[0])), &flag); code != 0 {
		return false, fmt.Errorf("TEN VAD process failed: %d", int(code))
	}
	return flag != 0, nil
}

func (d *nativeDetector) Close() error {
	if d.instance == nil {
		return nil
	}
	code := C.vad_close(d.instance)
	d.instance = nil
	if code != 0 {
		return fmt.Errorf("TEN VAD destroy failed: %d", int(code))
	}
	return nil
}
