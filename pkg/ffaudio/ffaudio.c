#include "ffaudio.h"
#include <stdlib.h>

#ifdef __linux__
#include <stdatomic.h>
#include "pulse_devices_linux.h"
#endif

static const ffaudio_interface* gFFI = &FFAUDIO_INTERFACE;

#ifdef __linux__
static atomic_int audio_initialized;
#endif

int audio_init(ffaudio_init_conf *conf) {
	int result = gFFI->init(conf);
#ifdef __linux__
	if (result == 0)
		atomic_store(&audio_initialized, 1);
#endif
	return result;
}

void audio_uninit() {
#ifdef __linux__
	atomic_store(&audio_initialized, 0);
#endif
	gFFI->uninit();
}

ffaudio_dev* audio_dev_alloc(ffuint mode) {
#ifdef __linux__
	return atomic_load(&audio_initialized) ? pulse_devices_alloc(mode) : NULL;
#else
	return gFFI->dev_alloc(mode);
#endif
}

void audio_dev_free(ffaudio_dev *d) {
	if (d) {
#ifdef __linux__
	    pulse_devices_free(d);
#else
	    gFFI->dev_free(d);
#endif
	}
}

const char* audio_dev_error(ffaudio_dev *d) {
#ifdef __linux__
	return pulse_devices_error(d);
#else
	return gFFI->dev_error(d);
#endif
}

int audio_dev_next(ffaudio_dev *d) {
#ifdef __linux__
	return pulse_devices_next(d);
#else
	return gFFI->dev_next(d);
#endif
}

const char* audio_dev_info(ffaudio_dev *d, ffuint i) {
#ifdef __linux__
	return pulse_devices_info(d, i);
#else
	return gFFI->dev_info(d, i);
#endif
}

void* audio_dev_info_DEV_ID(ffaudio_dev *d) {
	return (void*)audio_dev_info(d, FFAUDIO_DEV_ID);
}

void* audio_dev_info_LOOPBACK_ID(ffaudio_dev *d) {
#ifdef __linux__
	return (void*)pulse_devices_monitor(d);
#else
	return audio_dev_info_DEV_ID(d);
#endif
}

// 这里应该是 wasapi 专有
ffuint audio_dev_info_MIX_FORMAT_0(ffaudio_dev *d) {
	ffuint* a = (ffuint*)audio_dev_info(d, FFAUDIO_DEV_MIX_FORMAT);
	if (a == NULL) {
	    return 0;
	}
	return a[0];
}
ffuint audio_dev_info_MIX_FORMAT_1(ffaudio_dev *d) {
	ffuint* a = (ffuint*)audio_dev_info(d, FFAUDIO_DEV_MIX_FORMAT);
	if (a == NULL) {
	    return 0;
	}
	return a[1];
}
ffuint audio_dev_info_MIX_FORMAT_2(ffaudio_dev *d) {
	ffuint* a = (ffuint*)audio_dev_info(d, FFAUDIO_DEV_MIX_FORMAT);
	if (a == NULL) {
	    return 0;
	}
	return a[2];
}

// buf
ffaudio_buf* audio_alloc() {
    return gFFI->alloc();
}

void audio_free(ffaudio_buf *b) {
    if (b) {
        gFFI->free(b);
    }
}

const char* audio_error(ffaudio_buf *b) {
    return gFFI->error(b);
}

ffaudio_conf* ffaudio_conf_alloc() {
    ffaudio_conf* c = calloc(1, sizeof(ffaudio_conf));
    if (c == NULL) {
        return NULL;
    }
    c->format = FFAUDIO_F_INT16; // enum FFAUDIO_F
    c->sample_rate = 16000;
    c->channels = 1;
    c->buffer_length_msec = 512;
    c->device_id = NULL;
    c->app_name = NULL;
    return c;
}

void ffaudio_conf_free(ffaudio_conf* c) {
    if (c) {
        free(c);
    }
}

int audio_open(ffaudio_buf *b, ffaudio_conf *conf, ffuint flags) {
#ifdef __linux__
    ffaudio_conf capture_conf = *conf;
    if ((flags & 0x0f) == FFAUDIO_LOOPBACK) {
        if (capture_conf.device_id == NULL)
            capture_conf.device_id = "@DEFAULT_MONITOR@";
        flags = (flags & ~0x0f) | FFAUDIO_CAPTURE;
    }
    int result = gFFI->open(b, &capture_conf, flags);
    conf->format = capture_conf.format;
    conf->sample_rate = capture_conf.sample_rate;
    conf->channels = capture_conf.channels;
    conf->buffer_length_msec = capture_conf.buffer_length_msec;
    return result;
#else
    return gFFI->open(b, conf, flags);
#endif
}

int audio_start(ffaudio_buf *b) {
    return gFFI->start(b);
}

int audio_stop(ffaudio_buf *b) {
    return gFFI->stop(b);
}

int audio_clear(ffaudio_buf *b) {
    return gFFI->clear(b);
}

int audio_drain(ffaudio_buf *b) {
    return gFFI->drain(b);
}

struct audio_buff_data* audio_buff_data_alloc() {
    struct audio_buff_data* ab = calloc(1, sizeof(struct audio_buff_data));
    return ab;
}

void audio_buff_data_free(struct audio_buff_data* ab) {
    if (ab) {
        free(ab);
    }
}

int audio_write(ffaudio_buf *b, struct audio_buff_data *inbuf) {
    return gFFI->write(b, inbuf->data, (ffsize)inbuf->len);
}

int audio_read(ffaudio_buf *b, struct audio_buff_data *outbuf) {
    int rlen = gFFI->read(b, &outbuf->data);
    outbuf->len = (rlen > 0) ? rlen : 0;
    return rlen;
}
