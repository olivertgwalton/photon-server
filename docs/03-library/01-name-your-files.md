---
title: "Prepare folders and name files"
summary: "The folder layouts and file names the scanner reads as films, episodes, specials, versions, multi-part files and extras."
---

The scanner knows a title by its folder and file names before any provider is asked. A name it
reads well is matched at once; one it misreads is matched to the wrong title, or not at all. This
page sets out the layouts and names it reads. They are the ones Jellyfin and Plex read, so a
library laid out for either is already laid out for photon-server.

Keep films and shows apart, in two folders you add as two libraries, unless you want one library
of both (see [films and shows in one library](#films-and-shows-in-one-library)).

## Films

### One folder per film

Give each film a folder named for it, its year in brackets, and put the film inside:

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

A folder holding one film names it, whatever the file is called: `heat.1995.1080p.bluray.x264.mkv`
is Heat (1995) because its folder says so. The folder's pictures, NFO and theme tunes are that
film's too. Folders may nest as deep as you like: `/media/Films/Crime/Heat (1995)` reads the same.

### Loose files

A file at the library's root, or in a folder whose files are not all named after it, is a film of
its own, named by the file:

```text
/media/Films
├── Alien (1979).mkv
├── Aliens (1986).mkv
└── Halloween
    ├── Halloween (1978).mkv
    └── Halloween (2018).mkv
```

That is four films. Halloween (1978) and Halloween (2018) stay apart because their years differ,
and a name that only starts with the folder's, such as `Aliens.mkv` in a folder `Alien`, is a
different film.

### How a name is read

- **The year** is the last year in brackets, else the last bare one: `Blade Runner (1982)`,
  `Dune.2021.2160p.WEB-DL.mkv`. A name's first word is never its year, so `1917 (2019)` is 1917
  from 2019 and `2012.mkv` is 2012. A year that opens a date (`2013-12-09`) is not read as one.
- **The title** is what comes before the year. With no year it ends at the first release tag:
  a resolution (`1080p`, `2160p`), a source (`bluray`, `web-dl`, `webrip`, `hdtv`), a codec
  (`x264`, `x265`, `hevc`), or a word such as `proper`, `repack`, `remastered` or `unrated`.
- **Dots and underscores** become spaces in a name with no spaces: `The.Dark.Knight.2008.mkv` is
  The Dark Knight. A dot between single letters stays, so `S.H.I.E.L.D.` keeps its dots.
- **A release group** in square brackets at the start, `[Group] Title`, is dropped.

A year in the name is worth giving: a search without one, or with the wrong one, may find a
different film of the same name, and the server leaves a title unmatched rather than guess (see
[metadata](03-metadata.md#how-a-title-is-matched)).

### Naming the match yourself

A provider's id in a folder or file name decides the match outright, in square, round or curly
brackets, in Jellyfin's or Plex's spelling:

```text
Heat (1995) [tmdbid-949]
Heat (1995) {tmdb-949}
Heat (1995) [imdbid-tt0113277]
The Office (2005) [tvdbid-73244]
```

`tmdb`, `imdb` and `tvdb` are read, written `tmdbid-949`, `tmdb-949` or `tmdb=949`. An id in a
name outranks one in an NFO, as NFOs are mostly written by other tools; only a match you fix by hand
outranks it. The tag is not part of the title.

### Versions and editions

Several copies of one film, in its folder, are versions of it when every file starts with the
folder's name, gives the same year, and follows the name with a separator or a bracket. What
follows is the version's label:

```text
/media/Films/Blade Runner (1982)
├── Blade Runner (1982) - 2160p.mkv
├── Blade Runner (1982) - [1080p].mkv
└── Blade Runner (1982) {edition-Final Cut}.mkv
```

That is one film with three versions, labelled 2160p, 1080p and Final Cut. A Plex edition tag,
`{edition-…}`, is kept as the version's edition. A player offers the versions to choose from.

The folder's id tag is left out when comparing names, so the copies of `Heat (1995) [tmdbid-949]`
may be called `Heat (1995) - 2160p.mkv` and `Heat (1995) - 1080p.mkv`.

Byte-for-byte identical copies are one version kept in two places, wherever they are in the
library. The same film in two different folders is two titles: merge nothing by hand, and
[split apart](05-fix-a-match.md#split-a-film-apart) a title that holds copies that are not one
film.

### A film in several files

A film split across files is one copy played in order when each file's name ends with a part
marker: `cd`, `dvd`, `part`, `pt`, `disc` or `disk` and a number, or a letter from `a` to `d`:

```text
/media/Films/Lawrence of Arabia (1962)
├── Lawrence of Arabia (1962) cd1.mkv
└── Lawrence of Arabia (1962) cd2.mkv
```

The marker ends the name and follows a space, dot, underscore, hyphen or closing bracket, so
`Bad Boys (2006)` and `Bad Boys (2007)` are never read as two parts. Parts stack only with the
same marker and the same name before it.

## Shows

### One folder per show

A shows library holds one folder per show directly under its root, and the episodes under it,
in season folders or not:

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

The show is named by its folder, read as a film's is, its year and id tag included; a folder named
like a release, `The.Show.S01.1080p.WEB-DL`, names the show before the season. A video file at
the root of a shows library belongs to no show and is left out.

### Season folders

A folder under a show is a season when its name says so: `Season 1`, `S01`, `Series 2`,
`3. Staffel`, `Saison 4`, `Temporada 2`, and the word for season in many other languages, or a
bare number, `1`. The show's own name before it is allowed (`Seinfeld Season 2`). `Specials` is
season 0. Folders below a season, such as `Season 1/Disc 1`, belong to it.

A season folder is optional. An episode's name that states its season wins over its folder's;
an episode whose name states none takes its folder's season, and with no season folder either,
season 1.

### Episode names

The scanner tries these forms in order and takes the first that fits. The first group is read
with confidence:

| Form | Example |
| --- | --- |
| Season and episode | `The Wire S01E02.mkv`, `the.wire.s01.e02.mkv`, `S1E2` |
| Season x episode | `Doctor Who 1x01 - Rose.mkv` |
| In words | `Show Season 1 Episode 2.mkv`, `Show S1 E2.mkv` |
| Air date | `The Daily Show 2013-12-09.mkv`, `09.12.2013` |
| The word episode | `Episode 5.mkv` |
| E or EP and a number | `Show E05.mkv`, `Show EP05.mkv` |

The second group is a bare number, read as the episode where nothing better fits:

| Form | Example |
| --- | --- |
| Dash and number at the end, as anime is released | `Show - 101 [1080p].mkv` |
| A number in brackets | `[Group][Show][21][1080p].mkv` |
| Season and episode run together after the show's name, with a leading zero | `Seinfeld 0807 The Checks.mkv` |
| A number at the start | `01 - Pilot.mkv`, `01.Pilot.mkv` |
| A number between dashes | `Show - 01 - Pilot.mkv` |
| A number at the end | `One Piece 1001.mkv` |

What follows the season and episode is the episode's title until a provider names it:
`The Wire S01E01 - The Target.mkv` is called The Target, and an episode whose name says nothing
more is called Episode 1. Release tags after it are dropped.

A file whose name gives no episode at all is left out, and the server's log says so: "file left
out" with "its name says no season or episode".

### Several episodes in one file

A file holding several episodes names each, or the first and last:

```text
The Office S02E01E02.mkv
The Office S02E01-E02.mkv
The Office S02E01-02.mkv
The Office 2x01-02.mkv
01-02 - Pilot.mkv
```

A number lower than the one before it is part of the title, so `S03E21 - E2.mkv` is one episode
called E2, and a number followed by `p` or `i` is a resolution.

### Shows named by date

A daily show's episodes can be named by air date, year first or day first. One with no season in
its name or folder is filed under its year, as Plex files daily shows.
A day-first date that cannot be a day-first date is read month first.

### Absolute numbering

A show released with one running number, as anime often is, reads `Show - 101.mkv` as episode
101 of season 1. Set the show's episode order to Absolute so its provider numbers it the same way:
see [episode order](05-fix-a-match.md#episode-order).

### Copies of one episode

Two files with the same season and episode in a canonical form, such as `The Wire S01E01 -
1080p.mkv` and `The Wire S01E01 - 2160p.mkv`, are two copies of one episode. Two files read only
by a bare number are never joined: `Show - 01.mkv` twice is two episodes, so a misread name never
hides an episode behind another.

## Films and shows in one library

A library that holds films and shows, as Jellyfin's and Emby's mixed libraries do, tells a show's
folder from a film's by what is in it. A folder below the root is a show's when:

- it holds a `tvshow.nfo`, or
- a folder in it names a season in words (`Season 2`, `3. Staffel`; a bare `2`, `S02` or
  `Specials` could as well be a film's), or
- it holds at least two videos named as episodes of a season (`S01E01`, `1x02`), and no folder but
  seasons and extras.

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

Here The Wire and Fleabag are shows, and Alien and Heat films. Everything else is read as films,
so loose episodes beside show folders, `TV/Taskmaster S01E01.mkv`, are films; give them a folder.
A folder holding a single episode and no season folder is a film's, as Emby reads it, unless a
`tvshow.nfo` says otherwise.

## Extras

Trailers, featurettes and other extras sit beside their title, in a folder named for their kind or
with their kind at the end of the name.

| Folder | Suffix | Kind |
| --- | --- | --- |
| `Trailers` | `-trailer` | Trailer |
| `Featurettes` | `-featurette` | Featurette |
| `Behind The Scenes` | `-behindthescenes` | Behind the scenes |
| `Deleted Scenes` | `-deleted`, `-deletedscene` | Deleted scene |
| `Interviews` | `-interview` | Interview |
| `Scenes` | `-scene` | Scene |
| `Shorts` | `-short` | Short |
| `Clips` | `-clip` | Clip |
| `Backdrops` | | Theme video |
| `Extras`, `Extra`, `Other` | `-extra`, `-other` | Extra |

A suffix needs its hyphen, so a film whose title ends in Scene or Short is not taken for an extra.
A trailer may follow any separator, `Heat (1995).trailer.mkv`, or be called `trailer.mkv` alone.
A number after the kind is allowed: `Heat (1995)-trailer2.mkv`.

```text
/media/Films/Heat (1995)
├── Heat (1995).mkv
├── Heat (1995)-trailer.mkv
└── Featurettes
    └── Making Heat.mkv
```

A video in an extras folder belongs to the title of the folder above it and is named by its file.
A suffixed extra belongs to the film its name gives, or, in a folder of one film, to that film.

For a show, an extras folder in the show's folder holds the show's extras, and one in a season
folder the season's. A suffixed extra whose name gives an episode, `The Wire S01E01-deleted.mkv`,
is that episode's. Extras are never read as episodes.

## Pictures beside a title

A picture beside a title is used before any provider's, unless you choose another (see
[artwork](05-fix-a-match.md#choose-artwork)). The names are Jellyfin's and Kodi's. Pictures may be `.jpg`, `.jpeg`, `.png`, `.webp` or `.tbn`.

| Name | Is the title's |
| --- | --- |
| `poster`, `folder`, `cover`, `default`, `movie`, `show` | poster |
| `fanart`, `backdrop`, `background`, `art`, and numbered (`backdrop2`, `fanart-3`) | backdrop |
| `logo`, `clearlogo` | logo |
| `thumb`, `landscape` | landscape still |
| `banner` | banner |

A folder's own pictures (`poster.jpg`) are its film's where it holds one film, its show's in a
show's folder, and its season's in a season folder. In a folder of several films, name each
picture after its film: `Alien (1979)-poster.jpg`, or `Alien (1979).jpg` for its poster. An
episode's still is `The Wire S01E01.jpg` or `The Wire S01E01-thumb.jpg`. A season's pictures may
also sit in the show's folder as `season01-poster.jpg` or `season-specials-poster.jpg`.

## Subtitles and theme tunes

A subtitle file named after its video, `Heat (1995).en.forced.srt`, or in a `Subs` or `Subtitles`
folder beside it, is that video's; see [subtitles](10-subtitles.md) for the tags it reads. A
`theme.mp3` or a `theme-music` folder beside a title is its tune; see
[theme music](11-theme-music.md).

## What a library leaves out

- **File types**: only these are videos: `.3g2`, `.3gp`, `.amv`, `.asf`, `.avi`, `.divx`,
  `.dv`, `.dvr-ms`, `.f4v`, `.flv`, `.img`, `.iso`, `.m2t`, `.m2ts`, `.m2v`, `.m4v`, `.mk3d`,
  `.mkv`, `.mov`, `.mp4`, `.mpe`, `.mpeg`, `.mpg`, `.mts`, `.mxf`, `.nsv`, `.nuv`, `.ogm`, `.ogv`,
  `.qt`, `.rm`, `.rmvb`, `.strm`, `.tp`, `.ts`, `.vob`, `.webm`, `.wmv`, `.wtv` and `.xvid`.
- **Samples**: a `Sample` or `Samples` folder, and a file named `sample` or ending `-sample`,
  `.sample` or `_sample`.
- **Housekeeping**: any name starting with a dot (`.DS_Store`, `._` files, `.snapshot`), and
  `@eaDir`, `eaDir`, `#recycle`, `@Recycle`, `.@__thumb`, `$RECYCLE.BIN`,
  `System Volume Information`, `lost+found`, `metadata`, `extrafanart`, `extrathumbs`,
  `ps3_update`, `ps3_vprm`, `temprec`, `tempsbe` and `Thumbs.db`, in any case.
- **What a `.ignore` file hides**: an empty `.ignore` hides its whole folder. One with patterns
  hides what they match below it, in `.gitignore`'s syntax, as Jellyfin reads it: `*.part`,
  `Unsorted/`, `!keep.mkv`. A deeper `.ignore` takes over below it.

Links are followed wherever they lead, to files and folders, so a library of links into a remote
mount is a library like any other. A link back to a folder above it is left out, with the reason
in the log.

A `.strm` file is a shortcut, as Kodi, Jellyfin and Emby read one: its first line that is neither
blank nor a `#` comment is the address its media is at. Only an `http` or `https` address is read;
any other is not media, so the server never reads a path the library does not hold.
