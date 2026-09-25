#!/bin/sh
# Run inside the oaf-av image with this folder mounted at /work.
# Masters for sharing, and the smaller web encodes the site serves.
set -e
enc() {
  ffmpeg -v error -y -framerate 30 -i "/work/frames_$1/f%05d.jpg" -i "/work/mix_$1.wav" \
    -c:v libx264 -preset slow $3 -pix_fmt yuv420p -profile:v high \
    -c:a aac -b:a "$4" -movflags +faststart -shortest "/work/$2"
}
enc h master-trailer.mp4 "-crf 17" 256k
enc v master-reel.mp4 "-crf 17" 256k
enc h openagentfleet-trailer.mp4 "-crf 23 -maxrate 3500k -bufsize 7000k -tune film" 160k
enc v openagentfleet-reel.mp4 "-crf 23 -maxrate 3500k -bufsize 7000k -tune film" 160k
ls -la /work/*.mp4
