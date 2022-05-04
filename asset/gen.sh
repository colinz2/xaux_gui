#!/usr/bin/env bash

# https://www.iconfont.cn/
fyne bundle -pkg mytheme -name ResAppIcon -o ../src/mytheme/icons.go icon.png

fyne bundle -pkg mytheme -name ResSpeakerIcon -a -o ../src/mytheme/icons.go speaker.png
fyne bundle -pkg mytheme -name ResMicrophoneIcon -a -o ../src/mytheme/icons.go microphone.png
fyne bundle -pkg mytheme -name ResSettings -a -o ../src/mytheme/icons.go settings.png