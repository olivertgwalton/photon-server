<script lang="ts">
import { toast } from "svelte-sonner";
import Choice from "#lib/components/admin/Choice.svelte";
import * as Card from "#lib/components/ui/card/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Switch } from "#lib/components/ui/switch/index.js";
import {
	loadPreferences,
	type Preferences,
	type SkipMode,
	type SubtitleMode,
	savePreferences,
} from "#lib/player/preferences.js";
import { bitrate, language, qualities } from "#lib/player/words.js";

// Read in the browser alone: they are kept in it.
let prefs = $state<Preferences>(loadPreferences());

function set<K extends keyof Preferences>(key: K, value: Preferences[K]) {
	prefs = savePreferences({ [key]: value });
	toast.success("Saved for this browser.");
}

const tags = [
	"en",
	"fr",
	"de",
	"es",
	"it",
	"pt",
	"nl",
	"sv",
	"no",
	"da",
	"fi",
	"pl",
	"cs",
	"hu",
	"el",
	"ru",
	"uk",
	"tr",
	"ar",
	"he",
	"hi",
	"ja",
	"ko",
	"zh",
	"th",
	"vi",
	"id",
];
const languages = tags
	.map((value) => ({ value, label: language(value) }))
	.sort((a, b) => a.label.localeCompare(b.label));
const browser = language(navigator.language.split("-")[0]);

const qualityOptions = qualities.map((kbps) => ({
	value: String(kbps),
	label: kbps ? `Up to ${bitrate(kbps)}` : "Original: the file as it is",
}));
const subtitleModes: { value: SubtitleMode; label: string }[] = [
	{ value: "default", label: "As the file says" },
	{ value: "foreign", label: "When the sound is in another language" },
	{ value: "always", label: "Always" },
	{ value: "forced", label: "Only forced subtitles" },
	{ value: "off", label: "Never" },
];
const skipModes: { value: SkipMode; label: string }[] = [
	{ value: "button", label: "Show a button to skip" },
	{ value: "auto", label: "Skip by itself" },
	{ value: "off", label: "Don't offer to skip" },
];
</script>

<svelte:head><title>Playback · Settings · Photon</title></svelte:head>

<header class="grid max-w-2xl gap-1">
	<h1 class="title">Playback</h1>
	<p class="text-ink-2 text-sm">
		How this browser plays. These are kept in this browser, so another device
		has its own; choosing a track or quality in the player still wins for that
		title.
	</p>
</header>

<Card.Root class="max-w-2xl">
	<Card.Content>
		<Field.Group>
			<Field.Field>
				<Field.Label for="quality">Quality</Field.Label>
				<Choice
					id="quality"
					name="quality"
					options={qualityOptions}
					value={String(prefs.quality)}
					onchange={(v: string) => set("quality", Number(v))}
					class="w-full sm:w-72"
				/>
				<Field.Description>
					The most the server sends. Lower it on a slow connection; above it,
					the server converts the file down.
				</Field.Description>
			</Field.Field>
			<Field.Separator />
			<Field.Field>
				<Field.Label for="audio-language">Audio language</Field.Label>
				<Choice
					id="audio-language"
					name="audio-language"
					options={[
						{ value: "", label: "The file's own default" },
						...languages,
					]}
					value={prefs.audioLanguage}
					onchange={(v: string) => set("audioLanguage", v)}
					class="w-full sm:w-72"
				/>
				<Field.Description>
					Plays the track in this language where a title has one.
				</Field.Description>
			</Field.Field>
			<Field.Field>
				<Field.Label for="subtitle-mode">Subtitles</Field.Label>
				<Choice
					id="subtitle-mode"
					name="subtitle-mode"
					options={subtitleModes}
					value={prefs.subtitleMode}
					onchange={(v: SubtitleMode) => set("subtitleMode", v)}
					class="w-full sm:w-72"
				/>
				<Field.Description>
					When subtitles come on without being asked for. Forced subtitles
					translate only the lines in another language.
				</Field.Description>
			</Field.Field>
			<Field.Field>
				<Field.Label for="subtitle-language">Subtitle language</Field.Label>
				<Choice
					id="subtitle-language"
					name="subtitle-language"
					options={[
						{ value: "", label: `This browser's (${browser})` },
						...languages,
					]}
					value={prefs.subtitleLanguage}
					onchange={(v: string) => set("subtitleLanguage", v)}
					class="w-full sm:w-72"
				/>
			</Field.Field>
			<Field.Separator />
			<Field.Field orientation="horizontal">
				<Switch
					id="autoplay"
					checked={prefs.autoplay}
					onCheckedChange={(v) => set("autoplay", v)}
				/>
				<Field.Content>
					<Field.Label for="autoplay">Play the next episode</Field.Label>
					<Field.Description>
						When the credits roll, the next episode plays after a ten-second
						count. Off, it is offered and waits.
					</Field.Description>
				</Field.Content>
			</Field.Field>
			<Field.Field>
				<Field.Label for="skip-intro">Intros and recaps</Field.Label>
				<Choice
					id="skip-intro"
					name="skip-intro"
					options={skipModes}
					value={prefs.skipIntro}
					onchange={(v: SkipMode) => set("skipIntro", v)}
					class="w-full sm:w-72"
				/>
			</Field.Field>
			<Field.Field>
				<Field.Label for="skip-credits">Credits and previews</Field.Label>
				<Choice
					id="skip-credits"
					name="skip-credits"
					options={skipModes}
					value={prefs.skipCredits}
					onchange={(v: SkipMode) => set("skipCredits", v)}
					class="w-full sm:w-72"
				/>
				<Field.Description>
					Where the server has found where they are. Skipping the credits at the
					end of an episode goes on to the next.
				</Field.Description>
			</Field.Field>
		</Field.Group>
	</Card.Content>
</Card.Root>
