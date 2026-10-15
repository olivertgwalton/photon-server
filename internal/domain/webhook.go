package domain

// WebhookFormat is how a webhook's events are sent: photon's own signed JSON, or as a chat or push
// service takes a message, as Jellyfin's webhook plugin sends to each.
type WebhookFormat string

const (
	WebhookPhoton  WebhookFormat = "photon"
	WebhookDiscord WebhookFormat = "discord"
	WebhookSlack   WebhookFormat = "slack"
	WebhookNtfy    WebhookFormat = "ntfy"
	WebhookGotify  WebhookFormat = "gotify"
)

func WebhookFormats() []WebhookFormat {
	return []WebhookFormat{WebhookPhoton, WebhookDiscord, WebhookSlack, WebhookNtfy, WebhookGotify}
}
