#include "pulse_devices_linux.h"

#include <pulse/pulseaudio.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

struct pulse_device {
	struct pulse_device *next;
	char *id;
	char *name;
	char *display_name;
	char *monitor;
	int is_default;
};

struct pulse_devices {
	unsigned mode;
	int loaded;
	struct pulse_device *head;
	struct pulse_device *tail;
	struct pulse_device *current;
	char *default_id;
	char error[256];
};

static void devices_error(struct pulse_devices *devices, const char *operation, const char *message)
{
	if (devices->error[0] == '\0')
		snprintf(devices->error, sizeof(devices->error), "%s: %s", operation, message);
}

static const char *context_error(pa_context *context)
{
	int error = pa_context_errno(context);
	return error == PA_OK ? "PulseAudio operation failed" : pa_strerror(error);
}

static void device_free(struct pulse_device *device)
{
	free(device->id);
	free(device->name);
	free(device->display_name);
	free(device->monitor);
	free(device);
}

ffaudio_dev *pulse_devices_alloc(unsigned mode)
{
	if (mode != FFAUDIO_DEV_PLAYBACK && mode != FFAUDIO_DEV_CAPTURE)
		return NULL;
	struct pulse_devices *devices = calloc(1, sizeof(*devices));
	if (devices != NULL)
		devices->mode = mode;
	return (ffaudio_dev *)devices;
}

void pulse_devices_free(ffaudio_dev *device)
{
	struct pulse_devices *devices = (struct pulse_devices *)device;
	if (devices == NULL)
		return;
	struct pulse_device *next;
	for (struct pulse_device *item = devices->head; item != NULL; item = next) {
		next = item->next;
		device_free(item);
	}
	free(devices->default_id);
	free(devices);
}

static void on_server_info(pa_context *context, const pa_server_info *info, void *userdata)
{
	struct pulse_devices *devices = userdata;
	if (info == NULL) {
		devices_error(devices, "get server info", context_error(context));
		return;
	}
	const char *id = devices->mode == FFAUDIO_DEV_PLAYBACK
		? info->default_sink_name : info->default_source_name;
	if (id != NULL && (devices->default_id = strdup(id)) == NULL)
		devices_error(devices, "get server info", "out of memory");
}

static void add_device(struct pulse_devices *devices, const char *id, const char *name, const char *monitor)
{
	if (devices->error[0] != '\0')
		return;
	if (id == NULL) {
		devices_error(devices, "enumerate devices", "missing device ID");
		return;
	}
	struct pulse_device *item = calloc(1, sizeof(*item));
	if (item == NULL) {
		devices_error(devices, "enumerate devices", "out of memory");
		return;
	}
	item->id = strdup(id);
	item->name = strdup(name != NULL ? name : id);
	if (monitor != NULL)
		item->monitor = strdup(monitor);
	if (item->id == NULL || item->name == NULL || (monitor != NULL && item->monitor == NULL)) {
		device_free(item);
		devices_error(devices, "enumerate devices", "out of memory");
		return;
	}
	item->is_default = devices->default_id != NULL && strcmp(id, devices->default_id) == 0;
	if (devices->tail == NULL)
		devices->head = item;
	else
		devices->tail->next = item;
	devices->tail = item;
}

static void on_sink_info(pa_context *context, const pa_sink_info *info, int eol, void *userdata)
{
	struct pulse_devices *devices = userdata;
	if (eol < 0)
		devices_error(devices, "get playback devices", context_error(context));
	else if (eol == 0 && info != NULL)
		add_device(devices, info->name, info->description, info->monitor_source_name);
	else if (eol == 0)
		devices_error(devices, "get playback devices", "missing device info");
}

static void on_source_info(pa_context *context, const pa_source_info *info, int eol, void *userdata)
{
	struct pulse_devices *devices = userdata;
	if (eol < 0)
		devices_error(devices, "get capture devices", context_error(context));
	else if (eol == 0 && info == NULL)
		devices_error(devices, "get capture devices", "missing device info");
	else if (eol == 0 && info->monitor_of_sink == PA_INVALID_INDEX)
		add_device(devices, info->name, info->description, NULL);
}

static int name_in_use(struct pulse_devices *devices, struct pulse_device *device, const char *name)
{
	for (struct pulse_device *item = devices->head; item != NULL; item = item->next) {
		if (item != device && (strcmp(name, item->name) == 0
			|| (item->display_name != NULL && strcmp(name, item->display_name) == 0)))
			return 1;
	}
	return 0;
}

static int disambiguate_names(struct pulse_devices *devices)
{
	for (struct pulse_device *item = devices->head; item != NULL; item = item->next) {
		if (!name_in_use(devices, item, item->name))
			continue;
		unsigned suffix = 0;
		do {
			free(item->display_name);
			int length = suffix == 0
				? snprintf(NULL, 0, "%s [%s]", item->name, item->id)
				: snprintf(NULL, 0, "%s [%s] #%u", item->name, item->id, suffix);
			if (length < 0 || (item->display_name = malloc((size_t)length + 1)) == NULL) {
				item->display_name = NULL;
				devices_error(devices, "name devices", "out of memory");
				return -1;
			}
			if (suffix == 0)
				snprintf(item->display_name, (size_t)length + 1, "%s [%s]", item->name, item->id);
			else
				snprintf(item->display_name, (size_t)length + 1, "%s [%s] #%u", item->name, item->id, suffix);
			suffix++;
		} while (name_in_use(devices, item, item->display_name));
	}
	return 0;
}

static int iterate(pa_mainloop *loop, pa_context *context, struct pulse_devices *devices)
{
	if (pa_mainloop_iterate(loop, 1, NULL) < 0) {
		devices_error(devices, "PulseAudio main loop", "iteration failed");
		return -1;
	}
	if (!PA_CONTEXT_IS_GOOD(pa_context_get_state(context))) {
		devices_error(devices, "PulseAudio connection", context_error(context));
		return -1;
	}
	return 0;
}

static int wait_operation(pa_mainloop *loop, pa_context *context, pa_operation *operation,
	struct pulse_devices *devices, const char *name)
{
	if (operation == NULL) {
		devices_error(devices, name, context_error(context));
		return -1;
	}
	while (pa_operation_get_state(operation) == PA_OPERATION_RUNNING) {
		if (iterate(loop, context, devices) < 0) {
			pa_operation_cancel(operation);
			pa_operation_unref(operation);
			return -1;
		}
	}
	if (pa_operation_get_state(operation) != PA_OPERATION_DONE)
		devices_error(devices, name, "operation cancelled");
	pa_operation_unref(operation);
	return devices->error[0] == '\0' ? 0 : -1;
}

static int load_devices(struct pulse_devices *devices)
{
	int result = -1;
	pa_mainloop *loop = pa_mainloop_new();
	if (loop == NULL) {
		devices_error(devices, "create PulseAudio main loop", "out of memory");
		return -1;
	}
	pa_context *context = pa_context_new(pa_mainloop_get_api(loop), "xaux_gui");
	if (context == NULL) {
		devices_error(devices, "create PulseAudio context", "out of memory");
		goto done_loop;
	}
	if (pa_context_connect(context, NULL, 0, NULL) < 0) {
		devices_error(devices, "connect to PulseAudio", context_error(context));
		goto done_context;
	}
	while (pa_context_get_state(context) != PA_CONTEXT_READY) {
		if (iterate(loop, context, devices) < 0)
			goto done_context;
	}
	if (wait_operation(loop, context, pa_context_get_server_info(context, on_server_info, devices),
		devices, "get server info") < 0)
		goto done_context;
	pa_operation *operation = devices->mode == FFAUDIO_DEV_PLAYBACK
		? pa_context_get_sink_info_list(context, on_sink_info, devices)
		: pa_context_get_source_info_list(context, on_source_info, devices);
	if (wait_operation(loop, context, operation, devices, "get devices") < 0)
		goto done_context;
	result = disambiguate_names(devices);

done_context:
	pa_context_disconnect(context);
	pa_context_unref(context);
done_loop:
	pa_mainloop_free(loop);
	return result;
}

int pulse_devices_next(ffaudio_dev *device)
{
	struct pulse_devices *devices = (struct pulse_devices *)device;
	if (!devices->loaded) {
		devices->loaded = 1;
		if (load_devices(devices) < 0)
			return -FFAUDIO_ERROR;
		devices->current = devices->head;
	} else if (devices->error[0] != '\0') {
		return -FFAUDIO_ERROR;
	} else if (devices->current != NULL) {
		devices->current = devices->current->next;
	}
	return devices->current != NULL ? 0 : 1;
}

const char *pulse_devices_info(ffaudio_dev *device, unsigned property)
{
	struct pulse_devices *devices = (struct pulse_devices *)device;
	struct pulse_device *item = devices->current;
	if (item == NULL)
		return NULL;
	switch (property) {
	case FFAUDIO_DEV_ID:
		return item->id;
	case FFAUDIO_DEV_NAME:
		return item->display_name != NULL ? item->display_name : item->name;
	case FFAUDIO_DEV_IS_DEFAULT:
		return item->is_default ? item->id : NULL;
	default:
		return NULL;
	}
}

const char *pulse_devices_monitor(ffaudio_dev *device)
{
	struct pulse_devices *devices = (struct pulse_devices *)device;
	return devices->current != NULL ? devices->current->monitor : NULL;
}

const char *pulse_devices_error(ffaudio_dev *device)
{
	return ((struct pulse_devices *)device)->error;
}
