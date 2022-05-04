#!/usr/bin/env bash

# https://www.iconfont.cn/
# icon
fyne bundle -pkg mytheme -name ResAppIcon -o ./mytheme/icons.go icon.png

fyne bundle -pkg mytheme -name ResSpeakerIcon -a -o ./mytheme/icons.go speaker.png
fyne bundle -pkg mytheme -name ResMicrophoneIcon -a -o ./mytheme/icons.go microphone.png
fyne bundle -pkg mytheme -name ResSettings -a -o ./mytheme/icons.go settings.png

# font
# too big
# fyne bundle -pkg mytheme -name AliPuhuiTi -o ./mytheme/alipuhui1.go apr.ttf