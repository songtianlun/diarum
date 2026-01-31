<script lang="ts">
	export let title: string;
	export let backHref: string = '/diary';
	export let sticky: boolean = true;
</script>

<header class="glass border-b border-border/50 flex-shrink-0 z-20 {sticky ? 'sticky top-0' : ''}">
	<div class="max-w-4xl mx-auto px-4 h-11">
		<div class="flex items-center justify-between h-full">
			<div class="flex items-center gap-3">
				<a
					href={backHref}
					class="p-1.5 hover:bg-muted/50 rounded-lg transition-all duration-200"
					title="Back"
				>
					<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
					</svg>
				</a>
				<span class="text-lg font-semibold text-foreground">{title}</span>
				<slot name="subtitle" />
			</div>
			<div class="flex items-center gap-2">
				<slot name="actions" />
			</div>
		</div>
	</div>
</header>
