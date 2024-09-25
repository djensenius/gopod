# GoPod

This is a simple tool to record podcasts from online radio sources and build them into podcast feeds.

Requires `ffmpeg` to be installed.

## Config

Example config file:

Format is a JSON object with a `Podcasts` key that is an array of objects. Each object has the following keys:
- `Directory`: The directory to save the podcast to
- `Title`: The title of the podcast
- `ShortTitle`: A short title for the podcast that’s used in the filename
- `Image`: A URL to an image to use for podcast art
- `URL`: The URL base for the podcast feed. This is the URL that will be used to access the podcast feed and audio files
- `PodcastURL`: A link to the website for the podcast feed
- `SourceURL` The URL of the audio stream
- `Length`: The length of the podcast in seconds
- `Extension`: The extension of the audio file (`mp3` or `aac`)


Example config file:
```json
{
  "Podcasts": [
    {
      "Directory": "/Users/david/Downloads/podcasts/test",
      "Title": "Rare Frequencies",
      "ShortTitle": "rf",
      "Image": "https://farm1.staticflickr.com/742/21709331051_913ccb063a_m.jpg",
      "URL": "http://www.example.com/podcast1/",
      "PodcastURL": "https://spinitron.com/WZBC/show/792/Rare-Frequency",
      "SourceURL": "https://stream.wzbc.org/wzbc",
      "Length": 7320,
      "Extension": "mp3"
    },
    {
      "Directory": "/Users/david/Downloads/podcasts/test2",
      "Title": "Vocal Fry",
      "ShortTitle": "vf",
      "Image": "https://blogfiles.wfmu.org/VF/VFNWW_Final.jpg",
      "URL": "http://www.example.com/podcast2/",
      "PodcastURL": "https://www.wfmu.org/playlists/VF",
      "SourceURL": "https://stream0.wfmu.org/freeform-high.aac",
      "Length": 3720,
      "Extension": "aac"
    }
  ]
}
```

A file called `podcast.rss` is put in the same directory as the audio files.

## Use & example crontab

To use the app run:

```./gopod rf```

Replace `rf` with the short title of the podcast you want to record.

I activate the using crontabs. Here are some example crontabs to get you started:

```
59 21 * * 4 /home/david/Code/gopod/gopod rf
59 19 * * 2 /home/david/Code/gopod/gopod vf
```
