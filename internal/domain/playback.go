package domain

import (
	"time"
	"uuid"
)

// PlayMethod is how a title reaches the player.
type PlayMethod string

const (
	// PlayDirect is the file as it is.
	PlayDirect PlayMethod = "direct"
	// PlayRemux is the file's streams copied into HLS segments.
	PlayRemux PlayMethod = "remux"
	// PlayTranscode is the file encoded again for the player.
	PlayTranscode PlayMethod = "transcode"
)

func PlayMethods() []PlayMethod {
	return []PlayMethod{PlayDirect, PlayRemux, PlayTranscode}
}

// StoppedBy is what ended a playback.
type StoppedBy string

const (
	// StoppedByPlayer is its own player saying it stopped.
	StoppedByPlayer StoppedBy = "player"
	// StoppedByAdmin is an admin stopping it from the dashboard.
	StoppedByAdmin StoppedBy = "admin"
	// StoppedBySweep is the sweep of one its player stopped reporting.
	StoppedBySweep StoppedBy = "sweep"
)

func StoppedBys() []StoppedBy {
	return []StoppedBy{StoppedByPlayer, StoppedByAdmin, StoppedBySweep}
}

// PlayState is what the player says it is doing.
type PlayState string

const (
	StatePlaying PlayState = "playing"
	StatePaused  PlayState = "paused"
)

func PlayStates() []PlayState {
	return []PlayState{StatePlaying, StatePaused}
}

// Playback is one profile playing one copy of a title, from play to stop.
type Playback struct {
	ID       uuid.UUID
	Profile  uuid.UUID
	Item     uuid.UUID
	Version  uuid.UUID
	Method   PlayMethod
	State    PlayState
	Position time.Duration
	Started  time.Time
	Updated  time.Time
	// Length is how long the title runs, as it was when the playback started.
	Length time.Duration
	// Reached is the furthest it has got, so a play is counted once, as it first reaches the end.
	Reached Reach
	// Last is how far its player's last report got; none before the first.
	Last Reach
	// Tracks are those its player last said it plays with.
	Tracks ChosenTracks
	// Node is the server node running it, whose HLS it serves.
	Node uuid.UUID
	Card PlaybackCard
}

// NowPlaying is a playback as an admin's dashboard shows it, in the list of playbacks, the event
// stream's snapshot and each playback event alike.
type NowPlaying struct {
	ID uuid.UUID `json:"id"`
	PlaybackCard
	Method     PlayMethod `json:"method"`
	State      PlayState  `json:"state"`
	PositionMS int64      `json:"position_ms"`
	StartedAt  time.Time  `json:"started_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	NodeID     uuid.UUID  `json:"node_id"`
}

func (p Playback) Showing() NowPlaying {
	return NowPlaying{
		ID: p.ID, PlaybackCard: p.Card, Method: p.Method, State: p.State, PositionMS: p.Position.Milliseconds(),
		StartedAt: p.Started.UTC(), UpdatedAt: p.Updated.UTC(), NodeID: p.Node,
	}
}

// PlaybackCard is what an admin's dashboard shows of a playback beside where it has got to, as
// Jellyfin's session card does: who, on what, which title and copy, and how it plays. It is fixed
// as the playback starts, so listing playbacks decides nothing again, and kept and sent as JSON.
type PlaybackCard struct {
	Profile  PlaybackProfile   `json:"profile"`
	Device   PlaybackDevice    `json:"device"`
	Title    PlaybackTitle     `json:"title"`
	Version  PlaybackVersion   `json:"version"`
	Reasons  []TranscodeReason `json:"reasons,omitzero"`
	Video    *PlaybackVideo    `json:"video,omitzero"`
	Audio    *PlaybackAudio    `json:"audio,omitzero"`
	Subtitle *PlaybackSubtitle `json:"subtitle,omitzero"`
	// Acceleration is the device its video is encoded on, absent where it is not encoded.
	Acceleration Acceleration `json:"acceleration,omitzero"`
}

type PlaybackProfile struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// PlaybackDevice is the signed-in device that started a playback, as it named itself when it
// signed in, and the address it asked from.
type PlaybackDevice struct {
	ID      uuid.UUID `json:"id"`
	Name    string    `json:"name"`
	Client  string    `json:"client"`
	Address string    `json:"address"`
}

// PlaybackTitle is the title played, with its pictures by id; an episode's names its show and
// where in it it is.
type PlaybackTitle struct {
	ID            uuid.UUID `json:"id"`
	Kind          ItemKind  `json:"kind"`
	Title         string    `json:"title"`
	Year          int       `json:"year,omitzero"`
	ShowID        uuid.UUID `json:"show_id,omitzero"`
	Show          string    `json:"show,omitzero"`
	SeasonNumber  *int      `json:"season_number,omitzero"`
	EpisodeNumber *int      `json:"episode_number,omitzero"`
	EpisodeEnd    *int      `json:"episode_end,omitzero"`
	Poster        uuid.UUID `json:"poster,omitzero"`
	Thumb         uuid.UUID `json:"thumb,omitzero"`
	Backdrop      uuid.UUID `json:"backdrop,omitzero"`
}

type PlaybackVersion struct {
	ID          uuid.UUID `json:"id"`
	Edition     string    `json:"edition,omitzero"`
	Label       string    `json:"label,omitzero"`
	Container   string    `json:"container"`
	BitrateKbps int       `json:"bitrate_kbps,omitzero"`
	DurationMS  int64     `json:"duration_ms"`
}

// PlaybackVideo is the video stream played as it is in the file, and what it is encoded to;
// Encode is absent where it is copied.
type PlaybackVideo struct {
	Stream      int                 `json:"stream"`
	Codec       string              `json:"codec"`
	Profile     string              `json:"profile,omitzero"`
	Width       int                 `json:"width,omitzero"`
	Height      int                 `json:"height,omitzero"`
	Range       Range               `json:"range,omitzero"`
	BitrateKbps int                 `json:"bitrate_kbps,omitzero"`
	DolbyVision DolbyVisionHandling `json:"dolby_vision,omitzero"`
	Encode      *PlaybackEncode     `json:"encode,omitzero"`
}

// PlaybackAudio is the audio stream played as it is in the file, and what it is encoded to;
// Encode is absent where it is copied.
type PlaybackAudio struct {
	Stream      int             `json:"stream"`
	Codec       string          `json:"codec"`
	Language    string          `json:"language,omitzero"`
	Channels    int             `json:"channels,omitzero"`
	BitrateKbps int             `json:"bitrate_kbps,omitzero"`
	Encode      *PlaybackEncode `json:"encode,omitzero"`
}

// PlaybackEncode is what a stream is encoded to: a picture's size, the range it is sent in and
// whether HDR was tone mapped to reach it, or a sound's channels.
type PlaybackEncode struct {
	Codec       string `json:"codec"`
	Width       int    `json:"width,omitzero"`
	Height      int    `json:"height,omitzero"`
	Range       Range  `json:"range,omitzero"`
	Channels    int    `json:"channels,omitzero"`
	BitrateKbps int    `json:"bitrate_kbps,omitzero"`
	ToneMapped  bool   `json:"tone_mapped,omitzero"`
}

// PlaybackSubtitle is the subtitle shown, a stream of the file or a file beside it, and whether it
// is drawn into the video rather than by the player.
type PlaybackSubtitle struct {
	Stream   *int       `json:"stream,omitzero"`
	File     *uuid.UUID `json:"file,omitzero"`
	Codec    string     `json:"codec"`
	Language string     `json:"language,omitzero"`
	Burned   bool       `json:"burned,omitzero"`
}

// VideoPlan is the video stream played, by its index in the file, what becomes of its Dolby
// Vision, and what it is encoded to where it is not copied.
type VideoPlan struct {
	Stream      int
	Codec       string
	DolbyVision DolbyVisionHandling
	// Encode is the video encoded again; nil copies it.
	Encode *VideoEncode
}

// Burns is whether a subtitle is drawn into the picture.
func (v VideoPlan) Burns() bool {
	return v.Encode != nil && (v.Encode.Burn != nil || v.Encode.BurnFile != nil)
}

// VideoEncode is video encoded again: the codec, the picture's size, the most bitrate it may
// spend, the range it is encoded in, and whether HDR is tone mapped to SDR and an interlaced
// picture deinterlaced on the way.
type VideoEncode struct {
	Codec         VideoCodec
	Width, Height int
	BitrateKbps   int
	// Range is SDR, or the HDR10 or HLG of the source kept in HEVC's 10 bits.
	Range       Range
	ToneMap     bool
	Deinterlace bool
	// Burn is a subtitle stream of the file drawn into the picture, by its index: a picture, or
	// styled text. BurnFile is a styled subtitle file beside it drawn so.
	Burn     *int
	BurnFile *uuid.UUID
}

// VideoCodec is a codec the server encodes video to.
type VideoCodec string

const (
	VideoH264 VideoCodec = "h264"
	VideoHEVC VideoCodec = "hevc"
)

func VideoCodecs() []VideoCodec { return []VideoCodec{VideoH264, VideoHEVC} }

// HEVCEncoding is whether the server encodes video to HEVC for a client that plays it, as
// Jellyfin's AllowHevcEncoding, or only ever to H.264.
type HEVCEncoding string

const (
	HEVCAllow HEVCEncoding = "allow"
	HEVCDeny  HEVCEncoding = "deny"
)

func HEVCEncodings() []HEVCEncoding { return []HEVCEncoding{HEVCAllow, HEVCDeny} }

// DolbyVisionHandling is what a copy does with a stream's Dolby Vision.
type DolbyVisionHandling string

const (
	// DolbyVisionNone is a stream with none.
	DolbyVisionNone DolbyVisionHandling = ""
	// DolbyVisionKeep carries it to a client that shows it.
	DolbyVisionKeep DolbyVisionHandling = "keep"
	// DolbyVisionStrip drops it for the base layer, to a client that shows only that.
	DolbyVisionStrip DolbyVisionHandling = "strip"
)

// DolbyVisionHandlings are what a stream with Dolby Vision may have done with it.
func DolbyVisionHandlings() []DolbyVisionHandling {
	return []DolbyVisionHandling{DolbyVisionKeep, DolbyVisionStrip}
}

// AudioPlan is the audio stream played, by its index in the file, and what it is encoded to where
// it is not copied.
type AudioPlan struct {
	Stream int
	Encode *AudioEncode
}

// AudioEncode is audio encoded again: the codec, its channels and its bitrate.
type AudioEncode struct {
	Codec       string
	Channels    int
	BitrateKbps int
	// Boost multiplies its volume, as it is mixed down to stereo; 0 leaves it.
	Boost float64
}

// Acceleration is the device the server encodes video on.
type Acceleration string

const (
	AccelSoftware     Acceleration = "software"
	AccelVideoToolbox Acceleration = "videotoolbox"
	AccelVAAPI        Acceleration = "vaapi"
	AccelQSV          Acceleration = "qsv"
	AccelNVENC        Acceleration = "nvenc"
)

func Accelerations() []Acceleration {
	return []Acceleration{AccelSoftware, AccelVideoToolbox, AccelVAAPI, AccelQSV, AccelNVENC}
}

// TranscodeReason is why a copy cannot reach a client as it is, in Jellyfin's TranscodeReason terms.
type TranscodeReason string

const (
	ContainerNotSupported       TranscodeReason = "container_not_supported"
	VideoCodecNotSupported      TranscodeReason = "video_codec_not_supported"
	VideoProfileNotSupported    TranscodeReason = "video_profile_not_supported"
	VideoLevelNotSupported      TranscodeReason = "video_level_not_supported"
	VideoResolutionNotSupported TranscodeReason = "video_resolution_not_supported"
	VideoBitDepthNotSupported   TranscodeReason = "video_bit_depth_not_supported"
	VideoRangeNotSupported      TranscodeReason = "video_range_not_supported"
	AudioCodecNotSupported      TranscodeReason = "audio_codec_not_supported"
	AudioChannelsNotSupported   TranscodeReason = "audio_channels_not_supported"
	BitrateExceedsLimit         TranscodeReason = "bitrate_exceeds_limit"
	SubtitleCodecNotSupported   TranscodeReason = "subtitle_codec_not_supported"
	// PartsNotSupported is a copy in several files for a client that plays one.
	PartsNotSupported TranscodeReason = "parts_not_supported"
)

func TranscodeReasons() []TranscodeReason {
	return []TranscodeReason{
		ContainerNotSupported, VideoCodecNotSupported, VideoProfileNotSupported, VideoLevelNotSupported,
		VideoResolutionNotSupported, VideoBitDepthNotSupported, VideoRangeNotSupported, AudioCodecNotSupported,
		AudioChannelsNotSupported, BitrateExceedsLimit, SubtitleCodecNotSupported, PartsNotSupported,
	}
}

// PartPlayback is how a client plays a copy in several files: as one stream the server joins
// them into, or each file in turn from its own address.
type PartPlayback string

const (
	PartsJoined PartPlayback = "joined"
	PartsEach   PartPlayback = "each"
)

func PartPlaybacks() []PartPlayback { return []PartPlayback{PartsJoined, PartsEach} }

// SubtitleDelivery is how a client draws a subtitle itself, in Jellyfin's SubtitleDeliveryMethod
// terms: from inside the file it plays, or from a file of its own beside the video.
type SubtitleDelivery string

const (
	SubtitleEmbedded SubtitleDelivery = "embedded"
	SubtitleSidecar  SubtitleDelivery = "sidecar"
)

func SubtitleDeliveries() []SubtitleDelivery {
	return []SubtitleDelivery{SubtitleEmbedded, SubtitleSidecar}
}

// SegmentFormat is the container of a playback's HLS segments: fragmented MP4, or MPEG-TS for a
// player that takes nothing else, as many of Jellyfin's clients' transcoding profiles do.
type SegmentFormat string

const (
	SegmentsFMP4   SegmentFormat = "fmp4"
	SegmentsMPEGTS SegmentFormat = "mpegts"
)

func SegmentFormats() []SegmentFormat { return []SegmentFormat{SegmentsFMP4, SegmentsMPEGTS} }
