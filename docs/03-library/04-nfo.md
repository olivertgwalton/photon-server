---
title: "Use local NFO metadata"
summary: "Where photon-server looks for Kodi NFO files beside your media, what it reads from them, and how they rank against the providers."
---

An NFO is the XML file Kodi, Jellyfin, Emby and media managers such as Radarr, Sonarr and
tinyMediaManager write beside a film or episode to describe it. photon-server reads them as
Jellyfin does, so a library already described by one of those keeps its descriptions, its ids and
its locked fields. This page says where the server looks, what it takes, and how an NFO ranks
against the providers.

## Turn NFOs on or off

NFOs are a source like the providers: **Nfo** in a library's **Metadata downloaders**, at
Settings › Server › Libraries. A new library asks it first, above TMDB, as Jellyfin's default
order does, so what an NFO says wins over what TMDB says and TMDB fills the rest. Move **Nfo**
below TMDB to have TMDB win and the NFO fill only what TMDB leaves empty, or untick it to have the
server read no NFOs at all in that library.

An NFO holds no pictures for the server: pictures come from files beside the title and the image
fetchers (see [pictures](03-metadata.md#pictures)).

## Where the server looks

| Title | NFO |
| --- | --- |
| A film in a folder of its own | `Heat (1995).nfo`, named after its first copy's file, else `movie.nfo` |
| A film among others in a folder | its file's name with `.nfo`: `Alien (1979).nfo` |
| A show | `tvshow.nfo` in the show's folder |
| A season | `season.nfo` in the season's own folder |
| An episode | its file's name with `.nfo`: `The Wire S01E01.nfo` |

```text
/media/Shows/The Wire (2002)
├── tvshow.nfo
└── Season 1
    ├── season.nfo
    ├── The Wire S01E01.mkv
    └── The Wire S01E01.nfo
```

A film split across files takes the NFO named after its first part. In a library of films and
shows, a `tvshow.nfo` also marks its folder as a show's (see
[films and shows in one library](01-name-your-files.md#films-and-shows-in-one-library)).

An NFO is read when its folder is scanned. Editing or adding one changes the folder, so the next
scan of it, or the folder watch, reads it again. One that cannot be read is noted in the log as
"NFO not read" and the title goes on without it.

## What it reads

| Element | Read as |
| --- | --- |
| `title`, `originaltitle`, `sorttitle` | the title, its original title and the title it sorts by |
| `plot`, else `outline` | the write-up |
| `tagline` | the tagline |
| `mpaa` | the certificate, without a leading "Rated " |
| `premiered`, `releasedate`, `aired` | the release or air date, the first that is a date written `2006-01-02` |
| `year` | the year, else the date's |
| `genre` | genres, one to an element, or several in one split at a slash or a vertical bar |
| `studio` | studios, one to an element |
| `uniqueid type="tmdb"`, `"imdb"`, `"tvdb"` | that provider's id |
| `tmdbid`, `imdbid`, `imdb_id`, `tvdbid` | that provider's id |
| `id` | an IMDb id where it starts `tt`; otherwise TMDB's in a film's NFO and TheTVDB's in a show's |
| `season` or `seasonnumber`, `episode`, `episodenumberend` | an episode's or season's numbers |
| `namedseason number="2"` | in `tvshow.nfo`, a season's name |
| `lockdata`, `lockedfields` | fields no lower source may fill (see below) |

Nothing else is read: cast, ratings, artwork and sets in an NFO are left to the providers. The
first id given for a provider stands. An NFO is read up to 4 MiB, in the encoding it declares.

### Ids

An NFO's ids are how the server finds a title on its providers, ahead of any search: an NFO with a
`uniqueid` of TMDB is matched to that TMDB title. An id in the folder or file name outranks one in
an NFO, since NFOs are mostly written by other tools, and a match you fix by hand outranks both.

An NFO that is not XML at all, as some tools write, is read as a list of links: an IMDb id
(`tt0113277`), a `themoviedb.org/movie/949` or `/tv/` address, or a TheTVDB address with `?id=`.
Links after the XML are read the same way.

### Episode numbers

An episode NFO's numbers are stated rather than guessed from a name, so they win over the file's
name: `The Wire - 05.mkv` beside an NFO saying season 2, episode 1 is S02E01. The file's name must
still read as an episode for the file to be taken at all (see
[episode names](01-name-your-files.md#episode-names)). A file of several
episodes may hold several `episodedetails` elements; it is read as one episode numbered from the
lowest to the highest, its titles and write-ups joined with " / ".

### Locked fields

Jellyfin writes the fields an admin locked into its NFOs, and photon-server honours them. A field
locked in an NFO is claimed at the NFO's rank even where the NFO gives it no value, so no source
ranked below the NFO fills it.

- `lockdata` set to `true` locks every field.
- `lockedfields` names them, separated by `|`: `Name` (title, sort title and original title),
  `Overview` (write-up and tagline), `Genres`, `Studios` and `OfficialRating` (certificate).

```xml
<movie>
  <title>Heat</title>
  <plot>A group of professional bank robbers…</plot>
  <uniqueid type="tmdb" default="true">949</uniqueid>
  <uniqueid type="imdb">tt0113277</uniqueid>
  <lockedfields>Overview|Genres</lockedfields>
</movie>
```

A source ranked above the NFO is not held back by its locks. To hold a field against every source,
hold it on the title itself: see [edit its details](05-fix-a-match.md#edit-its-details).
