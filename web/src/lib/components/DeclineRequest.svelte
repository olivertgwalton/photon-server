<script lang="ts">
import type { components } from "#lib/api/schema.js";
import * as AlertDialog from "#lib/components/ui/alert-dialog/index.js";
import * as Field from "#lib/components/ui/field/index.js";
import { Textarea } from "#lib/components/ui/textarea/index.js";

type Request = components["schemas"]["Request"];

// Asks before a request is declined, and why, for whoever asked: open while
// there is a request to decline, and saying why is optional.
let {
	request = $bindable(),
	ondecline,
}: {
	request?: Request;
	ondecline: (request: Request, reason: string) => void;
} = $props();

const id = $props.id();
let reason = $state("");
$effect.pre(() => {
	if (request) reason = "";
});
</script>

<AlertDialog.Root
	bind:open={
		() => !!request,
		(open) => {
	if (!open) request = undefined;
}
	}
>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>Decline {request?.title}?</AlertDialog.Title>
			<AlertDialog.Description>
				{request?.profile}
				sees that it was declined, and why, if you say.
			</AlertDialog.Description>
		</AlertDialog.Header>
		<Field.Field>
			<Field.Label for="{id}-reason">Why</Field.Label>
			<Textarea
				id="{id}-reason"
				bind:value={reason}
				rows={2}
				placeholder="Optional"
			/>
		</Field.Field>
		<AlertDialog.Footer>
			<AlertDialog.Cancel>Cancel</AlertDialog.Cancel>
			<AlertDialog.Action
				onclick={() => {
					if (request) ondecline(request, reason.trim());
					request = undefined;
				}}
			>
				Decline
			</AlertDialog.Action>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
