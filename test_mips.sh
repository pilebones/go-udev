#!/bin/bash

cd $(dirname $0) || exit 1
GO_UDEV="go-udev-mips64"
GOOS=linux GOARCH=mips64 go build -o ${GO_UDEV}
chmod +x ./${GO_UDEV}
docker run -it --rm -v "$PWD:$PWD" -w $PWD dockcross/linux-mips ./${GO_UDEV} -info
# docker run -it --net=host -v=/dev:/dev --rm -v "$PWD:$PWD" -w $PWD dockcross/linux-mips ./${GO_UDEV} -monitor

