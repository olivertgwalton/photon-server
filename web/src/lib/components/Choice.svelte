<script lang="ts" generics="T extends string">
import * as Select from "#lib/components/ui/select/index.js";

// One of a few answers, sent with its form by `name` where it has one, and
// named by its trigger's label.
let {
	options,
	value = $bindable(),
	onchange,
	name,
	id,
	class: className,
	...labelled
}: {
	options: readonly { value: T; label: string; disabled?: boolean }[];
	value?: T;
	onchange?: (value: T) => void;
	name?: string;
	id?: string;
	class?: string;
	"aria-label"?: string;
	"aria-labelledby"?: string;
} = $props();

const label = $derived(options.find((o) => o.value === value)?.label);
</script>

<Select.Root
	type="single"
	{name}
	bind:value={value as string}
	onValueChange={(v) => onchange?.(v as T)}
>
	<Select.Trigger {id} class={className} {...labelled}>
		<span class="truncate">{label ?? "Choose"}</span>
	</Select.Trigger>
	<Select.Content>
		{#each options as option (option.value)}
			<Select.Item
				value={option.value}
				label={option.label}
				disabled={option.disabled}
			>
				{option.label}
			</Select.Item>
		{/each}
	</Select.Content>
</Select.Root>
