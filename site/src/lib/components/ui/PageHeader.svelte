<script lang="ts">
	export let title: string;
	export let backHref: string = '/diary';
	export let sticky: boolean = true;
</script>

<header class="glass border-b border-border/50 flex-shrink-0 z-20 {sticky ? 'sticky top-0' : ''}">
	<div class="max-w-6xl mx-auto px-4 h-11">
		<div class="grid grid-cols-[auto_1fr_auto] items-center gap-2 h-full">
			<div class="flex items-center gap-2 min-w-0">
				<a href="/" class="flex items-center gap-2 hover:opacity-80 transition-opacity" title="Diarum Home">
					<img src="/logo.png" alt="Diarum" class="w-6 h-6" />
					<span class="hidden sm:inline text-lg font-semibold text-foreground hover:text-primary transition-colors">Diarum</span>
				</a>
			</div>
			<div class="flex items-center justify-center min-w-0">
				<div class="text-sm font-medium text-foreground truncate">{title}</div>
				<slot name="subtitle" />
			</div>
			<div class="flex items-center justify-end gap-2 min-w-[2.5rem]">
				<a
					href={backHref}
					class="p-1.5 hover:bg-muted/50 rounded-lg transition-all duration-200"
					title="Diary"
				>
					<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" aria-hidden="true">
						<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
							d="M8 7V3m8 4V3m-9 8h10M5 21h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z" />
					</svg>
				</a>
				<slot name="actions" />
			</div>
		</div>
	</div>
</header>
