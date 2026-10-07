package playback

import (
	"slices"

	"github.com/olivertgwalton/photon-server/internal/domain"
	"github.com/olivertgwalton/photon-server/internal/store"
)

// Card is what the dashboard shows of a playback a session starts from address, of a copy, as
// decided, with the tracks asked for; its acceleration, where it is transcoded, is the encoder's to say.
func Card(s domain.Session, address string, t domain.PlaybackTitle, c store.PlayCopy, d Decision, tracks domain.ChosenTracks) domain.PlaybackCard {
	card := domain.PlaybackCard{
		Profile: domain.PlaybackProfile{ID: s.Profile.ID, Name: s.Profile.Name},
		Device: domain.PlaybackDevice{
			ID: s.ID, Name: s.Device, Client: s.Client, Address: address,
		},
		Title: t,
		Version: domain.PlaybackVersion{
			ID: c.Version, Edition: c.Edition, Label: c.Label, Container: domain.ContainerName(c.Container), BitrateKbps: c.BitrateKbps,
			DurationMS: c.DurationMS,
		},
		Reasons: d.Reasons,
	}
	stream := func(index int) domain.Stream {
		if i := slices.IndexFunc(c.Streams, func(s domain.Stream) bool { return s.Index == index }); i >= 0 {
			return c.Streams[i]
		}
		return domain.Stream{Index: index}
	}
	if v := d.Video; v != nil {
		src := stream(v.Stream)
		card.Video = &domain.PlaybackVideo{
			Stream: v.Stream, Codec: src.Codec, Profile: src.Profile, Width: src.Width, Height: src.Height,
			Range: src.Range, BitrateKbps: src.BitrateKbps, DolbyVision: v.DolbyVision,
		}
		if e := v.Encode; e != nil {
			card.Video.Encode = &domain.PlaybackEncode{
				Codec: string(e.Codec), Width: e.Width, Height: e.Height, Range: e.Range, BitrateKbps: e.BitrateKbps,
				ToneMapped: e.ToneMap,
			}
		}
	}
	if au := d.Audio; au != nil {
		src := stream(au.Stream)
		card.Audio = &domain.PlaybackAudio{
			Stream: au.Stream, Codec: src.Codec, Language: domain.TagOf(src.Language), Channels: src.Channels,
			BitrateKbps: src.BitrateKbps,
		}
		if e := au.Encode; e != nil {
			card.Audio.Encode = &domain.PlaybackEncode{Codec: e.Codec, Channels: e.Channels, BitrateKbps: e.BitrateKbps}
		}
	}
	burned := d.Video != nil && d.Video.Burns()
	switch {
	case tracks.Subtitle != nil:
		src := stream(*tracks.Subtitle)
		card.Subtitle = &domain.PlaybackSubtitle{
			Stream: tracks.Subtitle, Codec: src.Codec, Language: domain.TagOf(src.Language), Burned: burned,
		}
	case tracks.SubtitleFile != nil:
		if i := slices.IndexFunc(c.Subtitles, func(f store.PlaySubtitle) bool { return f.ID == *tracks.SubtitleFile }); i >= 0 {
			card.Subtitle = &domain.PlaybackSubtitle{
				File: tracks.SubtitleFile, Codec: c.Subtitles[i].Codec, Language: domain.TagOf(c.Subtitles[i].Language), Burned: burned,
			}
		}
	}
	return card
}
