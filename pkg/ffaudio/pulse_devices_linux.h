#ifndef XAUX_PULSE_DEVICES_LINUX_H
#define XAUX_PULSE_DEVICES_LINUX_H

#include "ffaudio.h"

ffaudio_dev *pulse_devices_alloc(unsigned mode);
void pulse_devices_free(ffaudio_dev *device);
int pulse_devices_next(ffaudio_dev *device);
const char *pulse_devices_info(ffaudio_dev *device, unsigned property);
const char *pulse_devices_monitor(ffaudio_dev *device);
const char *pulse_devices_error(ffaudio_dev *device);

#endif
