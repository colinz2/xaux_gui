#ifndef _FFAUDIO_H_
#define _FFAUDIO_H_

#include "ffaudio/audio.h"

typedef enum FFAUDIO_OPEN FFAUDIO_OPEN_TYPE;

void audio_init(ffaudio_init_conf *conf);
void audio_uninit();

ffaudio_dev* audio_dev_alloc(ffuint mode);
void audio_dev_free(ffaudio_dev *d);
const char* audio_dev_error(ffaudio_dev *d);
int audio_dev_next(ffaudio_dev *d);
const char* audio_dev_info(ffaudio_dev *d, ffuint i);

void* audio_dev_info_DEV_ID(ffaudio_dev *d);
//format
ffuint audio_dev_info_MIX_FORMAT_0(ffaudio_dev *d);
//SampleRate
ffuint audio_dev_info_MIX_FORMAT_1(ffaudio_dev *d);
//Channels
ffuint audio_dev_info_MIX_FORMAT_2(ffaudio_dev *d);

/// buf
ffaudio_buf* audio_alloc();
void audio_free(ffaudio_buf *b);
const char* audio_error(ffaudio_buf *b);

ffaudio_conf* ffaudio_conf_alloc();
void ffaudio_conf_free(ffaudio_conf*);

/** Open audio buffer
flags: enum FFAUDIO_OPEN
Return
  * 0: success
  * FFAUDIO_EFORMAT: input format isn't supported;  the supported format is set inside 'conf'
  * FFAUDIO_ERROR: call error() to get error message */
int audio_open(ffaudio_buf *b, ffaudio_conf *conf, ffuint flags);
int audio_start(ffaudio_buf *b);
int audio_stop(ffaudio_buf *b);
int audio_clear(ffaudio_buf *b);
int audio_drain(ffaudio_buf *b);

typedef struct audio_buff_data {
    const void* data;
    int len;
} audio_buff_data_t;

struct audio_buff_data* audio_buff_data_alloc();
void audio_buff_data_free(struct audio_buff_data* abd);

int audio_write(ffaudio_buf *b, struct audio_buff_data *inbuf);
int audio_read(ffaudio_buf *b, struct audio_buff_data* outbuf);

#endif