<script lang="ts">
import type { components } from "#lib/api/schema.js";
import Choice from "#lib/components/Choice.svelte";
import { Checkbox } from "#lib/components/ui/checkbox/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Switch } from "#lib/components/ui/switch/index.js";

let {
	access,
	libraries,
}: {
	access: components["schemas"]["Access"];
	libraries: { id: string; name: string }[];
} = $props();

// The ages certificates are for, as the server reads them.
const ages = [
	{ value: "any", label: "Any" },
	...[0, 6, 7, 9, 12, 13, 15, 16, 17, 18].map((age) => ({
		value: String(age),
		label: age ? `Up to ${age}` : "Suitable for all",
	})),
];
const age = $derived(access.max_age == null ? "any" : String(access.max_age));
const ageOptions = $derived(
	ages.some((a) => a.value === age)
		? ages
		: [...ages, { value: age, label: `Up to ${age}` }],
);

let every = $state(false);
$effect.pre(() => {
	every = access.libraries.length === 0;
});
</script>

<Field.Set>
	<Field.Legend>Libraries</Field.Legend>
	<Field.Field orientation="horizontal">
		<Switch id="every" bind:checked={every} />
		<Field.Label for="every"
			>Every library, including ones added later</Field.Label
		>
	</Field.Field>
	{#if !every}
		<div class="grid gap-2 sm:grid-cols-2">
			{#each libraries as library (library.id)}
				<Field.Field orientation="horizontal">
					<Checkbox
						id="library-{library.id}"
						name="libraries"
						value={library.id}
						checked={access.libraries.includes(library.id)}
					/>
					<Field.Label for="library-{library.id}">{library.name}</Field.Label>
				</Field.Field>
			{/each}
		</div>
	{/if}
</Field.Set>
<div class="grid gap-4 sm:grid-cols-2">
	<Field.Field>
		<Field.Label for="max_age">Certificates</Field.Label>
		<Choice id="max_age" name="max_age" value={age} options={ageOptions} />
	</Field.Field>
	<Field.Field>
		<Field.Label for="unrated">Titles with no certificate</Field.Label>
		<Choice
			id="unrated"
			name="unrated"
			value={access.unrated}
			options={[
				{ value: "allow", label: "Shown" },
				{ value: "block", label: "Hidden" },
			]}
		/>
	</Field.Field>
</div>
