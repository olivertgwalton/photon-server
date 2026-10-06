<script lang="ts">
import PageHeader from "#lib/components/PageHeader.svelte";
import { toast } from "svelte-sonner";
import { client } from "#lib/api/client.js";
import { problemMessage } from "#lib/api/problem.js";
import type { components } from "#lib/api/schema.js";
import Choice from "#lib/components/admin/Choice.svelte";
import * as Card from "#lib/components/ui/card/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Switch } from "#lib/components/ui/switch/index.js";
import type {
	Preferences,
	PreferencesChange,
} from "#lib/player/preferences.js";
import { bitrate, language, qualities } from "#lib/player/words.js";

type Schemas = components["schemas"];

let { data } = $props();
// Each change answers them as kept; a load gives them afresh.
let saved = $state<Preferences>();
const prefs = $derived(saved ?? data.prefs);

async function set(change: PreferencesChange) {
	const { data: kept, error } = await client().PATCH("/api/v1/me/preferences", {
		body: change,
	});
	if (error) {
		toast.error(problemMessage(error));
		return;
	}
	saved = kept;
	toast.success("Saved for every device.");
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
const qualityOptions = qualities.map((kbps) => ({
	value: String(kbps),
	label: kbps ? `Up to ${bitrate(kbps)}` : "Original: the file as it is",
}));
const subtitleModes: { value: Schemas["SubtitleMode"]; label: string }[] = [
	{ value: "default", label: "As the file says" },
	{ value: "smart", label: "When the sound is in another language" },
	{ value: "always", label: "Always" },
	{ value: "only_forced", label: "Only forced subtitles" },
	{ value: "none", label: "Never" },
];
const skipActions: { value: Schemas["SegmentAction"]; label: string }[] = [
	{ value: "ask", label: "Show a button to skip" },
	{ value: "skip", label: "Skip by itself" },
	{ value: "none", label: "Don't offer to skip" },
];
</script>

<PageHeader
	title="Playback"
	description="How you play, on every device you sign in on. Choosing a track or a quality in the player still wins for that title."
/>

<Card.Root class="max-w-2xl">
	<Card.Content>
		<Field.Group>
			<Field.Field>
				<Field.Label for="quality">Quality</Field.Label>
				<Choice
					id="quality"
					name="quality"
					options={qualityOptions}
					value={String(prefs.max_bitrate_kbps)}
					onchange={(v: string) => set({ max_bitrate_kbps: Number(v) })}
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
					options={[{ value: "", label: "Any" }, ...languages]}
					value={prefs.audio_language}
					onchange={(v: string) => set({ audio_language: v })}
					class="w-full sm:w-72"
				/>
			</Field.Field>
			<Field.Field orientation="horizontal">
				<Switch
					id="audio-default"
					checked={prefs.audio_track === "default"}
					onCheckedChange={(v) =>
						set({ audio_track: v ? "default" : "language" })}
				/>
				<Field.Content>
					<Field.Label for="audio-default">Play the default track</Field.Label>
					<Field.Description>
						The sound the file marks as its default, whatever its language. Off,
						the sound in your language plays where there is one.
					</Field.Description>
				</Field.Content>
			</Field.Field>
			<Field.Separator />
			<Field.Field>
				<Field.Label for="subtitle-mode">Subtitles</Field.Label>
				<Choice
					id="subtitle-mode"
					name="subtitle-mode"
					options={subtitleModes}
					value={prefs.subtitle_mode}
					onchange={(v: Schemas["SubtitleMode"]) => set({ subtitle_mode: v })}
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
					options={[{ value: "", label: "Any" }, ...languages]}
					value={prefs.subtitle_language}
					onchange={(v: string) => set({ subtitle_language: v })}
					class="w-full sm:w-72"
				/>
			</Field.Field>
			<Field.Separator />
			<Field.Field orientation="horizontal">
				<Switch
					id="remember-audio"
					checked={prefs.remember_audio === "remember"}
					onCheckedChange={(v) =>
						set({ remember_audio: v ? "remember" : "forget" })}
				/>
				<Field.Content>
					<Field.Label for="remember-audio"
						>Remember the sound chosen</Field.Label
					>
					<Field.Description>
						A title plays again with the sound you last chose for it.
					</Field.Description>
				</Field.Content>
			</Field.Field>
			<Field.Field orientation="horizontal">
				<Switch
					id="remember-subtitles"
					checked={prefs.remember_subtitles === "remember"}
					onCheckedChange={(v) =>
						set({ remember_subtitles: v ? "remember" : "forget" })}
				/>
				<Field.Content>
					<Field.Label for="remember-subtitles"
						>Remember the subtitles chosen</Field.Label
					>
					<Field.Description>
						A title plays again with the subtitles you last chose for it, or
						none.
					</Field.Description>
				</Field.Content>
			</Field.Field>
			<Field.Separator />
			<Field.Field orientation="horizontal">
				<Switch
					id="autoplay"
					checked={prefs.next_episode === "play"}
					onCheckedChange={(v) => set({ next_episode: v ? "play" : "offer" })}
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
					options={skipActions}
					value={prefs.intro_action}
					onchange={(v: Schemas["SegmentAction"]) => set({ intro_action: v })}
					class="w-full sm:w-72"
				/>
			</Field.Field>
			<Field.Field>
				<Field.Label for="skip-credits">Credits and previews</Field.Label>
				<Choice
					id="skip-credits"
					name="skip-credits"
					options={skipActions}
					value={prefs.credits_action}
					onchange={(v: Schemas["SegmentAction"]) => set({ credits_action: v })}
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
