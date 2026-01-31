<script lang="ts">
	import { createEventDispatcher } from 'svelte';

	export let disabled = false;
	export let placeholder = 'Type your message...';

	let content = '';
	const dispatch = createEventDispatcher<{ send: string }>();

	function handleSubmit() {
		if (content.trim() && !disabled) {
			dispatch('send', content.trim());
			content = '';
		}
	}

	function handleKeydown(e: KeyboardEvent) {
		if (e.key === 'Enter' && !e.shiftKey) {
			e.preventDefault();
			handleSubmit();
		}
	}
</script>

<form on:submit|preventDefault={handleSubmit} class="flex gap-2 items-end">
	<div class="flex-1 relative">
		<textarea
			bind:value={content}
			on:keydown={handleKeydown}
			{placeholder}
			{disabled}
			rows="1"
			class="w-full resize-none rounded-xl border border-border bg-background px-4 py-3 text-sm
				focus:outline-none focus:ring-2 focus:ring-primary/50 focus:border-primary
				disabled:opacity-50 disabled:cursor-not-allowed
				min-h-[48px] max-h-[200px]"
			style="field-sizing: content;"
		></textarea>
	</div>

	<button
		type="submit"
		disabled={disabled || !content.trim()}
		title="Send message"
		class="p-3 rounded-xl bg-primary text-primary-foreground
			hover:opacity-90 transition-opacity
			disabled:opacity-50 disabled:cursor-not-allowed
			flex-shrink-0"
	>
		<svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
			<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
				d="M12 19l9 2-9-18-9 18 9-2zm0 0v-8" />
		</svg>
	</button>
</form>
