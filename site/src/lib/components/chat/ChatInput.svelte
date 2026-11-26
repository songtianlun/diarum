<script lang="ts">
	import { createEventDispatcher, onMount, tick } from 'svelte';
	import { t } from '$lib/i18n';
	import type { ChatScope } from '$lib/api/chat';

	export let disabled = false;
	export let streaming = false;
	export let scope: ChatScope = 'all';
	export let placeholder = '';

	let content = '';
	let textareaEl: HTMLTextAreaElement;
	let isComposing = false;

	const dispatch = createEventDispatcher<{
		send: string;
		stop: void;
		scopeChange: ChatScope;
	}>();

	const scopes: Array<{ id: ChatScope; labelKey: string }> = [
		{ id: 'all', labelKey: 'assistant.scopeAll' },
		{ id: 'this_month', labelKey: 'assistant.scopeThisMonth' },
		{ id: 'this_year', labelKey: 'assistant.scopeThisYear' },
		{ id: 'none', labelKey: 'assistant.scopeNone' }
	];

	function resizeTextarea() {
		if (!textareaEl) return;
		textareaEl.style.height = 'auto';
		const nextHeight = Math.min(textareaEl.scrollHeight, 200);
		textareaEl.style.height = `${Math.max(nextHeight, 28)}px`;
	}

	async function handleSubmit() {
		if (streaming) {
			dispatch('stop');
			return;
		}
		const trimmed = content.trim();
		if (trimmed && !disabled) {
			dispatch('send', trimmed);
			content = '';
			await tick();
			resizeTextarea();
		}
	}

	function handleKeydown(e: KeyboardEvent) {
		// Prevent sending while an IME composition (Chinese/Japanese/Korean) is active
		if (e.isComposing || isComposing || e.keyCode === 229) {
			return;
		}
		if (e.key === 'Enter' && !e.shiftKey) {
			e.preventDefault();
			if (!streaming) {
				handleSubmit();
			}
		}
	}

	export function focus() {
		textareaEl?.focus();
	}

	export async function setDraft(text: string) {
		content = text;
		await tick();
		resizeTextarea();
		textareaEl?.focus();
	}

	onMount(() => {
		resizeTextarea();
	});
</script>

<form on:submit|preventDefault={handleSubmit} class="w-full">
	<div
		class="rounded-2xl border border-border bg-background/95 shadow-sm
			transition-all duration-150 focus-within:border-primary/60 focus-within:ring-2 focus-within:ring-primary/15"
	>
		<div class="px-3.5 pt-3 pb-1.5">
			<textarea
				bind:this={textareaEl}
				bind:value={content}
				on:input={resizeTextarea}
				on:keydown={handleKeydown}
				on:compositionstart={() => (isComposing = true)}
				on:compositionend={() => (isComposing = false)}
				placeholder={placeholder || $t('assistant.inputPlaceholder')}
				disabled={disabled && !streaming}
				rows="1"
				class="w-full resize-none bg-transparent text-sm leading-6 text-foreground
					placeholder:text-muted-foreground/70 focus:outline-none
					disabled:opacity-50 disabled:cursor-not-allowed max-h-[200px] overflow-y-auto"
			></textarea>
		</div>

		<div class="flex items-center justify-between gap-2 px-2.5 pb-2.5 pt-1">
			<!-- Context scope selector -->
			<div class="flex items-center gap-1 overflow-x-auto no-scrollbar py-0.5" role="group" aria-label={$t('assistant.scopeLabel')}>
				{#each scopes as item (item.id)}
					<button
						type="button"
						disabled={streaming}
						on:click={() => dispatch('scopeChange', item.id)}
						class="px-2.5 py-1 rounded-lg text-xs font-medium whitespace-nowrap transition-colors
							{scope === item.id
								? 'bg-primary/15 text-primary border border-primary/25'
								: 'text-muted-foreground hover:text-foreground hover:bg-muted/70 border border-transparent'}
							disabled:opacity-50"
					>
						{$t(item.labelKey)}
					</button>
				{/each}
			</div>

			<!-- Send / Stop button -->
			<div class="flex items-center gap-1.5 flex-shrink-0">
				{#if streaming}
					<button
						type="button"
						on:click={() => dispatch('stop')}
						title={$t('assistant.stop')}
						aria-label={$t('assistant.stop')}
						class="h-8 px-3 rounded-xl bg-foreground text-background
							hover:opacity-90 active:scale-95 transition-all duration-150
							flex items-center gap-1.5 text-xs font-medium shadow-sm"
					>
						<span class="w-2.5 h-2.5 rounded-[2px] bg-current"></span>
						<span class="hidden sm:inline">{$t('assistant.stop')}</span>
					</button>
				{:else}
					<button
						type="submit"
						disabled={disabled || !content.trim()}
						title={$t('assistant.send')}
						aria-label={$t('assistant.send')}
						class="h-8 w-8 rounded-xl bg-primary text-primary-foreground
							hover:opacity-90 active:scale-95 transition-all duration-150
							disabled:opacity-35 disabled:cursor-not-allowed disabled:active:scale-100
							flex items-center justify-center shadow-sm"
					>
						<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
							<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 12h14m-6-6 6 6-6 6" />
						</svg>
					</button>
				{/if}
			</div>
		</div>
	</div>
</form>

<style>
	.no-scrollbar::-webkit-scrollbar {
		display: none;
	}
	.no-scrollbar {
		-ms-overflow-style: none;
		scrollbar-width: none;
	}
</style>
