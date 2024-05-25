## 打包

```shell
fyne package -os darwin -icon ./asset/icon.png --name xaux_gui
fyne package -os linux -icon ./asset/icon.png --name xaux_gui
fyne package -os windows -icon ./asset/icon.png --name xaux_gui
```

## build

```shell
go build -ldflags "-H windowsgui"  .\src\main.go
```

### build deps
- msys2
#### 
```shell
cmake -G "Unix Makefiles" -DBUILD_SHARED_LIBS=ON -DCMAKE_INSTALL_PREFIX=./local ../
```
## 参考：

https://juejin.cn/post/7087845871777218567

dialog:
https://github.com/sqweek/dialog

theme生成器
https://github.com/lusingander/fyne-theme-generator