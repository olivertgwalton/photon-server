<script lang="ts">
import { untrack } from "svelte";
import { toast } from "svelte-sonner";
import { refreshAll } from "$app/navigation";
import { accessOf } from "#lib/admin/access.js";
import { client } from "#lib/api/client.js";
import { problemMessage } from "#lib/api/problem.js";
import type { components } from "#lib/api/schema.js";
import { fields } from "#lib/form.js";
import Choice from "#lib/components/Choice.svelte";
import AccessFields from "#lib/components/admin/AccessFields.svelte";
import { Button } from "#lib/components/ui/button/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Input } from "#lib/components/ui/input/index.js";

type Provider = components["schemas"]["AdminSignInProvider"];
type Provisioning = components["schemas"]["Provisioning"];
type Recheck = components["schemas"]["Recheck"];

// Registers the server on a provider, or changes how: a new one is named by
// the slug its redirect URI carries, which an existing one keeps.
let {
	provider,
	publicURL,
	libraries,
	onsaved,
}: {
	provider?: Provider;
	publicURL: string;
	libraries: { id: string; name: string }[];
	onsaved: () => void;
} = $props();

const id = (field: string) => `${provider?.slug ?? "new"}-${field}`;
// The form starts from the provider as it is, and is the admin's from then.
let slug = $state(untrack(() => provider?.slug ?? ""));
let provisioning = $state<Provisioning>(
	untrack(() => provider?.provisioning ?? "link"),
);
let recheck = $state<Recheck>(untrack(() => provider?.recheck ?? "hourly"));
let refused = $state<string>();
let pending = $state(false);

const provisionings: { value: Provisioning; label: string }[] = [
	{ value: "link", label: "Only accounts linked to a profile" },
	{ value: "create", label: "Anyone it signs in, each given a profile" },
];

const rechecks: { value: Recheck; label: string }[] = [
	{ value: "hourly", label: "Every hour" },
	{ value: "at_sign_in", label: "Only as they sign in" },
];

const redirect = $derived(
	provider?.callback_url ??
		(publicURL && slug
			? `${publicURL.replace(/\/+$/, "")}/api/v1/auth/sign-in-providers/${slug}/callback`
			: ""),
);

async function copy() {
	await navigator.clipboard.writeText(redirect);
	toast.success("The redirect URI was copied.");
}

async function save(event: SubmitEvent) {
	const form = fields(event);
	const text = (name: string) => String(form.get(name) ?? "").trim();
	pending = true;
	const { data: saved, error } = await client().PUT(
		"/api/v1/admin/sign-in-providers/{slug}",
		{
			params: { path: { slug: provider?.slug ?? slug } },
			body: {
				name: text("name"),
				issuer: text("issuer"),
				client_id: text("client_id"),
				client_secret: text("client_secret") || undefined,
				provisioning,
				group: text("group") || undefined,
				recheck,
				access: provisioning === "create" ? accessOf(form) : undefined,
			},
		},
	);
	pending = false;
	refused = error ? problemMessage(error) : undefined;
	if (!saved) return;
	toast.success(`${saved.name} was saved.`);
	onsaved();
	await refreshAll();
}
</script>

<form onsubmit={save}>
	<Field.Group>
		{#if !provider}
			<Field.Field>
				<Field.Label for={id("slug")}>Slug</Field.Label>
				<Input
					id={id("slug")}
					name="slug"
					required
					maxlength={32}
					pattern="[a-z0-9][a-z0-9\-]*"
					autocomplete="off"
					spellcheck="false"
					placeholder="pocket-id"
					class="font-mono"
					bind:value={slug}
				/>
				<Field.Description>
					Lower-case letters, digits and hyphens. It names the provider in the
					redirect URI, so it can't be changed later.
				</Field.Description>
			</Field.Field>
		{/if}
		{#if redirect}
			<Field.Field>
				<Field.Label for={id("redirect")}>Redirect URI</Field.Label>
				<div class="flex gap-2">
					<Input
						id={id("redirect")}
						value={redirect}
						readonly
						class="font-mono"
					/>
					<Button type="button" variant="outline" onclick={copy}>Copy</Button>
				</div>
				<Field.Description>
					Register it with the provider as the client's redirect URI.
				</Field.Description>
			</Field.Field>
		{/if}
		<Field.Field>
			<Field.Label for={id("name")}>Name</Field.Label>
			<Input
				id={id("name")}
				name="name"
				required
				maxlength={64}
				placeholder="Pocket ID"
				value={provider?.name ?? ""}
			/>
			<Field.Description>
				The login page offers to continue with it by this name.
			</Field.Description>
		</Field.Field>
		<Field.Field>
			<Field.Label for={id("issuer")}>Issuer</Field.Label>
			<Input
				id={id("issuer")}
				name="issuer"
				type="url"
				required
				spellcheck="false"
				placeholder="https://id.example.com"
				class="font-mono"
				value={provider?.issuer ?? ""}
			/>
			<Field.Description>
				The provider's address, exactly as its discovery document names it.
			</Field.Description>
		</Field.Field>
		<Field.Field>
			<Field.Label for={id("client-id")}>Client id</Field.Label>
			<Input
				id={id("client-id")}
				name="client_id"
				required
				autocomplete="off"
				spellcheck="false"
				class="font-mono"
				value={provider?.client_id ?? ""}
			/>
		</Field.Field>
		<Field.Field>
			<Field.Label for={id("client-secret")}>Client secret</Field.Label>
			<Input
				id={id("client-secret")}
				name="client_secret"
				type="password"
				autocomplete="off"
				class="font-mono"
				placeholder={provider?.client_secret_set
					? "Kept: leave empty to keep it"
					: ""}
			/>
			<Field.Description>
				{provider?.client_secret_set
					? "Leave it empty to keep the secret kept, unless the client id changes."
					: "Leave it empty for a public client, which has none."}
			</Field.Description>
		</Field.Field>
		<Field.Field>
			<Field.Label id={id("provisioning-label")}>Who signs in</Field.Label>
			<Choice
				options={provisionings}
				bind:value={provisioning}
				aria-labelledby={id("provisioning-label")}
				class="w-full"
			/>
			<Field.Description>
				{provisioning === "create"
					? "Someone in the group below signing in for the first time gets a profile of their own, which sees what you choose here."
					: "Each person links their account in their profile's settings before they can sign in with it."}
			</Field.Description>
		</Field.Field>
		<Field.Field>
			<Field.Label for={id("group")}>Required group</Field.Label>
			<Input
				id={id("group")}
				name="group"
				required={provisioning === "create"}
				autocomplete="off"
				spellcheck="false"
				value={provider?.group ?? ""}
			/>
			<Field.Description>
				{provisioning === "create" ? "Required." : "Optional."}
				Only accounts in this group at the provider, as its groups claim says,
				can sign in.
			</Field.Description>
		</Field.Field>
		<Field.Field>
			<Field.Label id={id("recheck-label")}>Check back</Field.Label>
			<Choice
				options={rechecks}
				bind:value={recheck}
				aria-labelledby={id("recheck-label")}
				class="w-full"
			/>
			<Field.Description>
				{recheck === "hourly"
					? "Each hour the server asks the provider whether each account may still sign in, and signs out the devices of one it has disabled or that has left the group. The provider must offer offline access. Its refresh tokens must last longer than the server might be down: raise Authelia's from its 90 minutes, or choose to check only as people sign in."
					: "Accounts are checked as they sign in and not after: removing someone at the provider doesn't sign out their devices, so sign them out under Devices too."}
			</Field.Description>
		</Field.Field>
		{#if provisioning === "create"}
			<AccessFields
				access={provider?.access ?? {
					libraries: [],
					max_age: null,
					unrated: "allow",
				}}
				{libraries}
			/>
		{/if}
		<Field.Error errors={[{ message: refused }]} />
		<Field.Field orientation="horizontal">
			<Button type="submit" disabled={pending}>
				{provider ? "Save" : "Add provider"}
			</Button>
		</Field.Field>
	</Field.Group>
</form>
