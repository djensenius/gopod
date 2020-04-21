# GoPod
Automatically create podcast feed from directory of audio files. GoPod watches a directory for when files are posted. Written in Go.

## Config

Edit the config sample file and move it to `~/.config/gopod/config.json`.

A file called `podcast.rss` is put in the same directory as the audio files.

## Example

I use [StreamRipper](http://streamripper.sourceforge.net) to record radio stations on the internet. I then use this program to create a podcast feed. My crontab looks like this:

```
00 19 * * THU streamripper http://amber.streamguys.com:4860/listen.pls -a -A -s -l 10800 -d /home/david/podcasts/tmp/ -u "Mozilla/5.0 (iPhone; CPU iPhone OS 13_3_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/13.0.5 Mobile/15E148 Safari/604.1"; mv /home/david/podcasts/tmp/* /home/david/podcasts/rf/
00 19 * * 1 streamripper http://wfmu.org/wfmu.pls -a -A -s -l 3600 -d /home/david/podcasts/tmp/ -u "Mozilla/5.0 (iPhone; CPU iPhone OS 13_3_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/13.0.5 Mobile/15E148 Safari/604.1"; mv /home/david/podcasts/tmp/* /home/david/podcasts/vf/
```

I trust you can set up an httpd server to serve the files. 💪
