#!/usr/bin/env bash

# https://www.iconfont.cn/
# icon
fyne bundle -pkg mytheme -name ResAppIcon -o ./mytheme/icons.go ./asset/icon.png

fyne bundle -pkg mytheme -name ResSpeakerIcon -a -o ./mytheme/icons.go ./asset/speaker.png
fyne bundle -pkg mytheme -name ResMicrophoneIcon -a -o ./mytheme/icons.go ./asset/microphone.png
fyne bundle -pkg mytheme -name ResSettings -a -o ./mytheme/icons.go ./asset/settings.png

fyne bundle -pkg mytheme -name ResBegin -a -o ./mytheme/icons.go ./asset/begin.png
fyne bundle -pkg mytheme -name ResEnd -a -o ./mytheme/icons.go ./asset/end.png

# font
# too big
# fyne bundle -pkg mytheme -name AliPuhuiTi -o ./mytheme/alipuhui1.go apr.ttf