---
title: "Prepare folders and name files"
summary: "How to lay out and name films, shows, versions and extras so photon-server recognises them."
---

photon-server works out what each file is from its folder and file name, before it looks anything
up online. A clear name is recognised straight away. A muddled one may be matched to the wrong
title, or not at all.

The layouts on this page are the same ones Jellyfin and Plex use. If your files are already set up
for either, they are already set up for photon-server.

Keep films and shows in two separate folders, and add each as its own library. You can mix them in
one library if you prefer: see [films and shows in one library](#films-and-shows-in-one-library).

## Films

Give each film a folder named after it, with its year in brackets, and put the film inside:

```text
/media/Films
├── Blade Runner (1982)
│   ├── Blade Runner (1982).mkv
│   ├── poster.jpg
│   └── fanart.jpg
├── Heat (1995)
│   ├── heat.1995.1080p.bluray.x264.mkv
│   └── heat.1995.1080p.bluray.x264.en.srt
└── 1917 (2019)
    └── 1917 (2019).mp4
```

When a folder holds one film, the folder's name is what counts. The file inside Heat (1995) can be
called anything. Pictures, subtitles and other files in the folder belong to that film too.
Folders can sit as deep as you like: `/media/Films/Crime/Heat (1995)` works the same.

Films can also sit loose, each named after itself:

```text
/media/Films
├── Alien (1979).mkv
├── Aliens (1986).mkv
└── Halloween
    ├── Halloween (1978).mkv
    └── Halloween (2018).mkv
```

That is four films. The two Halloweens stay apart because their years differ.

### Tips for film names

- **Include the year.** Without it, the server may pick a different film with the same name.
- **Messy download names are fine.** Dots and underscores are read as spaces, and anything after
  the year, such as `1080p` or `bluray`, is ignored. `The.Dark.Knight.2008.1080p.mkv` is The Dark
  Knight (2008).
- **A group name in square brackets at the start**, such as `[Group] Title`, is ignored.

### Naming the match yourself

If you know a film's or show's ID on TMDB, TheTVDB or IMDb, add it to the folder name. The server
then uses that exact title and skips searching:

```text
Heat (1995) [tmdbid-949]
Heat (1995) {tmdb-949}
Heat (1995) [imdbid-tt0113277]
The Office (2005) [tvdbid-73244]
Cowboy Bebop (1998) [anidbid=23]
```

You can put the ID in square, round or curly brackets. This is the surest way to get a tricky
title right. Only a match you fix by hand in the web app beats it.

### Versions and editions

Put several versions of one film in its folder, each starting with the folder's name. Whatever
comes after the name becomes the version's label:

```text
/media/Films/Blade Runner (1982)
├── Blade Runner (1982) - 2160p.mkv
├── Blade Runner (1982) - [1080p].mkv
└── Blade Runner (1982) {edition-Final Cut}.mkv
```

That is one film with three versions, labelled 2160p, 1080p and Final Cut. When you press play, you
can choose which one.

## Shows

Give each show a folder directly inside the library, and put its episodes in season folders:

```text
/media/Shows
├── The Wire (2002)
│   ├── poster.jpg
│   ├── season01-poster.jpg
│   ├── Season 1
│   │   ├── The Wire S01E01 - The Target.mkv
│   │   └── The Wire S01E02 - The Detail.mkv
│   └── Season 2
│       └── The Wire S02E01 - Ebb Tide.mkv
├── Fleabag
│   ├── Fleabag S01E01.mkv
│   └── Fleabag S01E02.mkv
└── Doctor Who (2005)
    ├── Specials
    │   └── Doctor Who S00E01 - The Christmas Invasion.mkv
    └── Series 1
        └── Doctor Who 1x01 - Rose.mkv
```

The show takes its name from its folder. Season folders are optional, and can be called
`Season 1`, `Series 1`, `S01`, `1`, or "season" in many other languages. `Specials` is season 0.

### Episode names

The safest name gives the season and episode as `S01E02`. These all work:

| Form | Example |
| --- | --- |
| Season and episode | `The Wire S01E02.mkv`, `the.wire.s01.e02.mkv` |
| Season x episode | `Doctor Who 1x01 - Rose.mkv` |
| In words | `Show Season 1 Episode 2.mkv` |
| Air date | `The Daily Show 2013-12-09.mkv` |
| Just a number | `Show - 101.mkv`, `01 - Pilot.mkv` |

Anything after the season and episode is used as the episode's name until the online lookup gives
the proper one. A file whose name gives no episode number is skipped, and the server's log says so.

## Films and shows in one library

A library that holds both tells a show's folder from a film's by what is inside. A folder counts as
a show when it holds a `tvshow.nfo` file, a season folder named in words (`Season 2`), or at least
two files named as episodes (`S01E01`).

```text
/media/Everything
├── Alien (1979).mkv
├── Films
│   └── Heat (1995)
│       └── Heat (1995).mkv
└── TV
    ├── The Wire (2002)
    │   └── Season 1
    │       └── The Wire S01E01.mkv
    └── Fleabag
        ├── Fleabag S01E01.mkv
        └── Fleabag S01E02.mkv
```

Here The Wire and Fleabag are shows, and Alien and Heat are films. Everything else is treated as a
film, so give every show its own folder.

## Extras

Trailers, featurettes and other extras go beside their title, either in a folder named for their
kind or with the kind at the end of the file name:

```text
/media/Films/Heat (1995)
├── Heat (1995).mkv
├── Heat (1995)-trailer.mkv
└── Featurettes
    └── Making Heat.mkv
```

| Folder | Or end the name with |
| --- | --- |
| `Trailers` | `-trailer` |
| `Featurettes` | `-featurette` |
| `Behind The Scenes` | `-behindthescenes` |
| `Deleted Scenes` | `-deleted` |
| `Interviews` | `-interview` |
| `Scenes` | `-scene` |
| `Shorts` | `-short` |
| `Extras` | `-extra` |

For a show, an extras folder in the show's folder belongs to the show, and one in a season folder
belongs to that season.

## Pictures beside a title

A picture you put beside a title is used before any picture found online. Use these names, as
`.jpg`, `.png` or `.webp`:

| Name | Used as |
| --- | --- |
| `poster`, `folder` or `cover` | the poster |
| `fanart` or `backdrop` | the background picture |
| `logo` or `clearlogo` | the title's logo |
| `thumb` or `landscape` | a wide still |
| `banner` | the banner |

In a folder holding several films, start each picture with its film's name:
`Alien (1979)-poster.jpg`. An episode's still is named after the episode:
`The Wire S01E01.jpg`. A season's poster can sit in the show's folder as `season01-poster.jpg`.

Subtitle files and theme tunes go beside a title too: see [subtitles](10-subtitles.md) and
[theme music](11-theme-music.md).

## Good to know

- **A film in several files.** End each name with `cd1`, `cd2`, `part1`, `disc1` and so on, and
  they play in order as one film: `Lawrence of Arabia (1962) cd1.mkv`.
- **Several episodes in one file.** Name both: `The Office S02E01E02.mkv` or `S02E01-02.mkv`.
- **Shows numbered straight through**, as anime often is (`Show - 101.mkv`), need the show's
  episode order set to **Absolute**: see [episode order](05-fix-a-match.md#episode-order).
- **Daily shows** named only by date are filed under the year as the season.
- **What is skipped.** Sample files and folders, hidden files that start with a dot, NAS system
  folders such as `@eaDir` and `#recycle`, and anything that is not a video file.
- **Hiding a folder.** Put an empty file called `.ignore` in it. A `.ignore` with lines in it hides
  only the files those lines match, written the same way as a `.gitignore` file.
- **Shortcuts.** Links to files and folders elsewhere are followed. A `.strm` file, a text file
  holding a web address, plays what is at that address.
