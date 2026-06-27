#!/bin/sh
set -eu

mediamtx /mediamtx.yml &
sleep 1

# Publish looping RTSP for cam-b from first sequence image.
if [ -f /images/seq-001.jpg ]; then
  ffmpeg -hide_banner -loglevel error -re -stream_loop -1 -i /images/seq-001.jpg \
    -f rtsp -rtsp_transport tcp "rtsp://127.0.0.1:8554/cam-b" &
fi

exec /usr/local/bin/camera-simulator
