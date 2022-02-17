#!/bin/bash

cd $(dirname $0) || exit 1
GO_UDEV="go-udev-amd64"
GOOS=linux GOARCH=amd64 go build -o ${GO_UDEV}
chmod +x ./${GO_UDEV}
docker run -it --net=host -v=/dev:/dev -v /run/udev:/run/udev:ro --rm -v "$PWD:$PWD" -w $PWD debian ./${GO_UDEV} -monitor
# docker run -it --net=host -v=/dev:/dev --rm -v "$PWD:$PWD" -w $PWD debian udevadm monitor

