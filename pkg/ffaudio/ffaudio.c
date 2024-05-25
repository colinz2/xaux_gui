#include "ffaudio.h"
#include <stdlib.h>

static const ffaudio_interface* gFFI = &FFAUDIO_INTERFACE;

void audio_init(ffaudio_init_conf *conf) {
	gFFI->init(conf);
}

void audio_uninit() {
	gFFI->uninit();
}

ffaudio_dev* audio_dev_alloc(ffuint mode) {
	return gFFI->dev_alloc(mode);
}

void audio_dev_free(ffaudio_dev *d) {
	if (d) {
	    gFFI->dev_free(d);
	}
}

const char* audio_dev_error(ffaudio_dev *d) {
	return gFFI->dev_error(d);
}

int audio_dev_next(ffaudio_dev *d) {
	return gFFI->dev_next(d);
}

const char* audio_dev_info(ffaudio_dev *d, ffuint i) {
	return gFFI->dev_info(d, i);
}

void* audio_dev_info_DEV_ID(ffaudio_dev *d) {
	return (void*)(gFFI->dev_info(d, FFAUDIO_DEV_ID));
}

// 这里应该是 wasapi 专有
ffuint audio_dev_info_MIX_FORMAT_0(ffaudio_dev *d) {
	ffuint* a = (ffuint*)gFFI->dev_info(d, FFAUDIO_DEV_MIX_FORMAT);
	if (a == NULL) {
	    return 0;
	}
	return a[0];
}
ffuint audio_dev_info_MIX_FORMAT_1(ffaudio_dev *d) {
	ffuint* a = (ffuint*)gFFI->dev_info(d, FFAUDIO_DEV_MIX_FORMAT);
	if (a == NULL) {
	    return 0;
	}
	return a[1];
}
ffuint audio_dev_info_MIX_FORMAT_2(ffaudio_dev *d) {
	ffuint* a = (ffuint*)gFFI->dev_info(d, FFAUDIO_DEV_MIX_FORMAT);
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
    ffaudio_conf* c = malloc(sizeof(ffaudio_conf));
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
    return gFFI->open(b, conf, flags);
}

int audio_start(ffaudio_buf *b) {
    return gFFI->start(b);
}

int audio_stop(ffaudio_buf *b) {
    return gFFI->start(b);
}

int audio_clear(ffaudio_buf *b) {
    return gFFI->start(b);
}

int audio_drain(ffaudio_buf *b) {
    return gFFI->start(b);
}

struct audio_buff_data* audio_buff_data_alloc() {
    struct audio_buff_data* ab = malloc(sizeof(struct audio_buff_data));
    ab->data = NULL;
    ab->len = 0;
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
    outbuf->len = rlen;
    return rlen;
}

