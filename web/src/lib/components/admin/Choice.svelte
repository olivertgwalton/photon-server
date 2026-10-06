<script lang="ts" generics="T extends string">
import * as Select from "#lib/components/ui/select/index.js";

// One of a few answers, sent with its form by `name`.
let {
	id,
	name,
	options,
	value = $bindable(),
	onchange,
	class: className,
}: {
	id: string;
	name: string;
	options: readonly { value: T; label: string }[];
	value?: T;
	onchange?: (value: T) => void;
	class?: string;
} = $props();

const label = $derived(options.find((o) => o.value === value)?.label);
</script>

<Select.Root
	type="single"
	{name}
	bind:value={value as string}
	onValueChange={(v) => onchange?.(v as T)}
>
	<Select.Trigger {id} class={className}>{label ?? "Choose"}</Select.Trigger>
	<Select.Content>
		{#each options as option (option.value)}
			<Select.Item value={option.value} label={option.label}>
				{option.label}
			</Select.Item>
		{/each}
	</Select.Content>
</Select.Root>
