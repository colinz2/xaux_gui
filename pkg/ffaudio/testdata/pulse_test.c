#include <assert.h>
#include <string.h>
#include <unistd.h>

#include "../ffaudio.h"
extern const ffaudio_interface test_interface;
#include "../ffaudio.c"
#include "../pulse_devices_linux.c"

static unsigned opened_flags;
static const char *opened_device;

static int fake_open(ffaudio_buf *buf, ffaudio_conf *conf, unsigned flags)
{
	opened_flags = flags;
	opened_device = conf->device_id;
	conf->sample_rate = 48000;
	if (conf->app_name == NULL)
		conf->app_name = "ffaudio";
	return FFAUDIO_EFORMAT;
}

const ffaudio_interface test_interface = {
	.open = fake_open,
};

static void test_loopback_mapping(void)
{
	ffaudio_conf conf = {0};
	assert(audio_open(NULL, &conf, FFAUDIO_LOOPBACK | FFAUDIO_O_NONBLOCK) == FFAUDIO_EFORMAT);
	assert(opened_flags == (FFAUDIO_CAPTURE | FFAUDIO_O_NONBLOCK));
	assert(strcmp(opened_device, "@DEFAULT_MONITOR@") == 0);
	assert(conf.device_id == NULL);
	assert(conf.app_name == NULL);
	assert(conf.sample_rate == 48000);

	conf.device_id = "chosen.monitor";
	assert(audio_open(NULL, &conf, FFAUDIO_LOOPBACK) == FFAUDIO_EFORMAT);
	assert(strcmp(opened_device, "chosen.monitor") == 0);
	assert(opened_flags == FFAUDIO_CAPTURE);

	conf.device_id = "microphone";
	assert(audio_open(NULL, &conf, FFAUDIO_CAPTURE) == FFAUDIO_EFORMAT);
	assert(strcmp(opened_device, "microphone") == 0);
	assert(opened_flags == FFAUDIO_CAPTURE);
	assert(conf.app_name == NULL);
}

static void test_devices(void)
{
	ffaudio_dev *playback_handle = pulse_devices_alloc(FFAUDIO_DEV_PLAYBACK);
	assert(playback_handle != NULL);
	struct pulse_devices *playback = (struct pulse_devices *)playback_handle;
	pa_server_info server = {0};
	server.default_sink_name = "sink2";
	on_server_info(NULL, &server, playback);

	pa_sink_info sink = {0};
	sink.name = "sink1";
	sink.description = "First speaker";
	sink.monitor_source_name = "actual.monitor.name";
	on_sink_info(NULL, &sink, 0, playback);
	sink.name = "sink2";
	sink.description = "Default speaker";
	sink.monitor_source_name = "other.monitor";
	on_sink_info(NULL, &sink, 0, playback);

	playback->current = playback->head;
	assert(strcmp(pulse_devices_info(playback_handle, FFAUDIO_DEV_ID), "sink1") == 0);
	assert(strcmp(audio_dev_info_LOOPBACK_ID(playback_handle), "actual.monitor.name") == 0);
	assert(pulse_devices_info(playback_handle, FFAUDIO_DEV_IS_DEFAULT) == NULL);
	playback->current = playback->current->next;
	assert(strcmp(pulse_devices_info(playback_handle, FFAUDIO_DEV_NAME), "Default speaker") == 0);
	assert(pulse_devices_info(playback_handle, FFAUDIO_DEV_IS_DEFAULT) != NULL);

	sink.name = "sink3";
	sink.description = "First speaker";
	sink.monitor_source_name = "third.monitor";
	on_sink_info(NULL, &sink, 0, playback);
	sink.name = "sink4";
	sink.description = "First speaker [sink1]";
	sink.monitor_source_name = "fourth.monitor";
	on_sink_info(NULL, &sink, 0, playback);
	assert(disambiguate_names(playback) == 0);
	playback->current = playback->head;
	assert(strcmp(pulse_devices_info(playback_handle, FFAUDIO_DEV_NAME), "First speaker [sink1] #1") == 0);
	playback->current = playback->head->next->next;
	assert(strcmp(pulse_devices_info(playback_handle, FFAUDIO_DEV_NAME), "First speaker [sink3]") == 0);
	playback->current = playback->current->next;
	assert(strcmp(pulse_devices_info(playback_handle, FFAUDIO_DEV_NAME), "First speaker [sink1]") == 0);

	ffaudio_dev *capture_handle = pulse_devices_alloc(FFAUDIO_DEV_CAPTURE);
	assert(capture_handle != NULL);
	struct pulse_devices *capture = (struct pulse_devices *)capture_handle;
	server.default_source_name = "mic2";
	on_server_info(NULL, &server, capture);
	pa_source_info source = {0};
	source.name = "monitor";
	source.monitor_of_sink = 1;
	on_source_info(NULL, &source, 0, capture);
	assert(capture->head == NULL);
	source.name = "mic2";
	source.description = "Default microphone";
	source.monitor_of_sink = PA_INVALID_INDEX;
	on_source_info(NULL, &source, 0, capture);
	capture->current = capture->head;
	assert(strcmp(pulse_devices_info(capture_handle, FFAUDIO_DEV_ID), "mic2") == 0);
	assert(pulse_devices_info(capture_handle, FFAUDIO_DEV_IS_DEFAULT) != NULL);

	pulse_devices_free(playback_handle);
	pulse_devices_free(capture_handle);
}

static void test_connection_failure(void)
{
	char directory[] = "/tmp/xaux-ffaudio-XXXXXX";
	assert(mkdtemp(directory) != NULL);
	char server[sizeof(directory) + 32];
	assert(snprintf(server, sizeof(server), "unix:%s/missing", directory) > 0);
	assert(setenv("PULSE_SERVER", server, 1) == 0);
	ffaudio_dev *devices = pulse_devices_alloc(FFAUDIO_DEV_CAPTURE);
	assert(devices != NULL);
	assert(pulse_devices_next(devices) < 0);
	assert(strlen(pulse_devices_error(devices)) > 0);
	assert(pulse_devices_next(devices) < 0);
	pulse_devices_free(devices);
	assert(rmdir(directory) == 0);
}

int main(void)
{
	test_loopback_mapping();
	test_devices();
	test_connection_failure();
	return 0;
}
