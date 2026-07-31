<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { getToday } from '$lib/utils/date';
	import { isAuthenticated } from '$lib/api/client';
	import { getGeneralSettings } from '$lib/api/settings';
	import Footer from '$lib/components/ui/Footer.svelte';
	import { t } from '$lib/i18n';

	let ready = $state(false);

	onMount(() => {
		if ($isAuthenticated) {
			void (async () => {
				let target = `/diary/${getToday()}`;
				try {
					const settings = await getGeneralSettings();
					if (settings.homepage === 'overview') {
						target = '/diary';
					}
				} catch {
					// fall back to today's entry
				}
				goto(target).catch(() => {
					ready = true;
				});
			})();
		} else {
			ready = true;
		}
	});

	const featureIcons = ['📝', '🤖', '📅', '🔍', '🖼️', '🌙'];
	let features = $derived(
		featureIcons.map((icon, i) => ({
			icon,
			title: $t(`landing.feature${i + 1}Title`),
			description: $t(`landing.feature${i + 1}Desc`)
		}))
	);
</script>

{#if !ready}
	<div class="flex items-center justify-center min-h-screen">
		<p class="text-muted-foreground">{$t('common.loading')}</p>
	</div>
{:else}
	<div class="min-h-screen flex flex-col bg-background">
		<!-- Navigation -->
		<nav class="fixed top-0 left-0 right-0 z-50 glass border-b border-border/50">
			<div class="max-w-6xl mx-auto px-4 sm:px-6 lg:px-8">
				<div class="flex items-center justify-between h-16">
					<div class="flex items-center gap-2">
						<img src="/logo.png" alt="Diarum" class="w-8 h-8" />
						<span class="text-2xl font-bold text-foreground">Diarum</span>
					</div>
					<a
						href="/login"
						class="px-4 py-2 text-sm font-medium text-foreground hover:text-primary transition-colors"
					>
						{$t('landing.login')}
					</a>
				</div>
			</div>
		</nav>

		<!-- Hero Section -->
		<section class="pt-32 pb-20 px-4 sm:px-6 lg:px-8">
			<div class="max-w-4xl mx-auto text-center animate-fade-in">
				<h1 class="text-4xl sm:text-5xl lg:text-6xl font-bold text-foreground mb-6 leading-tight">
					{$t('landing.heroTitlePrefix')}
					<span class="text-primary">{$t('landing.heroTitleHighlight')}</span>
				</h1>
				<p class="text-lg sm:text-xl text-muted-foreground mb-8 max-w-2xl mx-auto">
					{$t('landing.heroSubtitle')}
				</p>
				<div class="flex flex-col sm:flex-row items-center justify-center gap-4">
					<a
						href="/login"
						class="w-full sm:w-auto px-8 py-3 text-lg font-medium bg-primary text-primary-foreground rounded-xl hover:opacity-90 transition-all shadow-lg hover:shadow-xl"
					>
						{$t('landing.startWriting')}
					</a>
					<a
						href="#features"
						class="w-full sm:w-auto px-8 py-3 text-lg font-medium text-foreground border border-border rounded-xl hover:bg-accent transition-all"
					>
						{$t('landing.learnMore')}
					</a>
				</div>
			</div>
		</section>

		<!-- Screenshots Section -->
		<section class="py-16 px-4 sm:px-6 lg:px-8 bg-muted/30">
			<div class="max-w-7xl mx-auto">
				<!-- Desktop Screenshots -->
				<div class="hidden md:block mb-12 animate-fade-in">
					<div class="relative rounded-2xl overflow-hidden shadow-2xl border border-border/50">
						<!-- Light Mode Screenshot -->
						<img
							src="/screenshots/desktop-light.png"
							alt={$t('landing.desktopAlt')}
							class="w-full h-auto dark:hidden"
							loading="lazy"
						/>
						<!-- Dark Mode Screenshot -->
						<img
							src="/screenshots/desktop-dark.png"
							alt={$t('landing.desktopAlt')}
							class="w-full h-auto hidden dark:block"
							loading="lazy"
						/>
					</div>
				</div>

				<!-- Mobile Screenshots -->
				<div class="md:hidden mb-8 animate-fade-in">
					<div class="relative rounded-2xl overflow-hidden shadow-2xl border border-border/50 max-w-sm mx-auto">
						<!-- Light Mode Screenshot -->
						<img
							src="/screenshots/mobile-light.png"
							alt={$t('landing.mobileAlt')}
							class="w-full h-auto dark:hidden"
							loading="lazy"
						/>
						<!-- Dark Mode Screenshot -->
						<img
							src="/screenshots/mobile-dark.png"
							alt={$t('landing.mobileAlt')}
							class="w-full h-auto hidden dark:block"
							loading="lazy"
						/>
					</div>
				</div>

				<!-- Feature Highlights -->
				<div class="grid grid-cols-1 md:grid-cols-3 gap-6 mt-12">
					<div class="text-center p-6 bg-card/50 rounded-xl border border-border/30">
						<div class="w-12 h-12 mx-auto mb-4 rounded-xl bg-primary/10 flex items-center justify-center">
							<svg class="w-6 h-6 text-primary" fill="none" stroke="currentColor" viewBox="0 0 24 24">
								<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z"/>
							</svg>
						</div>
						<h3 class="font-semibold text-foreground mb-2">{$t('landing.highlightEditorTitle')}</h3>
						<p class="text-sm text-muted-foreground">{$t('landing.highlightEditorDesc')}</p>
					</div>

					<div class="text-center p-6 bg-card/50 rounded-xl border border-border/30">
						<div class="w-12 h-12 mx-auto mb-4 rounded-xl bg-primary/10 flex items-center justify-center">
							<svg class="w-6 h-6 text-primary" fill="none" stroke="currentColor" viewBox="0 0 24 24">
								<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 7V3m8 4V3m-9 8h10M5 21h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v12a2 2 0 002 2z"/>
							</svg>
						</div>
						<h3 class="font-semibold text-foreground mb-2">{$t('landing.highlightCalendarTitle')}</h3>
						<p class="text-sm text-muted-foreground">{$t('landing.highlightCalendarDesc')}</p>
					</div>

					<div class="text-center p-6 bg-card/50 rounded-xl border border-border/30">
						<div class="w-12 h-12 mx-auto mb-4 rounded-xl bg-primary/10 flex items-center justify-center">
							<svg class="w-6 h-6 text-primary" fill="none" stroke="currentColor" viewBox="0 0 24 24">
								<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 18h.01M8 21h8a2 2 0 002-2V5a2 2 0 00-2-2H8a2 2 0 00-2 2v14a2 2 0 002 2z"/>
							</svg>
						</div>
						<h3 class="font-semibold text-foreground mb-2">{$t('landing.highlightResponsiveTitle')}</h3>
						<p class="text-sm text-muted-foreground">{$t('landing.highlightResponsiveDesc')}</p>
					</div>
				</div>

				<p class="mt-6 text-center text-sm text-muted-foreground">
					{$t('landing.screenshotsCaption')}
				</p>
			</div>
		</section>

		<!-- Features Section -->
		<section id="features" class="py-20 px-4 sm:px-6 lg:px-8">
			<div class="max-w-6xl mx-auto">
				<div class="text-center mb-16 animate-fade-in">
					<h2 class="text-3xl sm:text-4xl font-bold text-foreground mb-4">
						{$t('landing.featuresTitle')}
					</h2>
					<p class="text-lg text-muted-foreground max-w-2xl mx-auto">
						{$t('landing.featuresSubtitle')}
					</p>
				</div>

				<div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
					{#each features as feature, i}
						<div
							class="p-6 bg-card rounded-xl border border-border/50 hover:border-primary/30 hover:shadow-lg transition-all duration-300"
							style="animation-delay: {i * 100}ms"
						>
							<div class="text-4xl mb-4">{feature.icon}</div>
							<h3 class="text-xl font-semibold text-foreground mb-2">{feature.title}</h3>
							<p class="text-muted-foreground">{feature.description}</p>
						</div>
					{/each}
				</div>
			</div>
		</section>

		<!-- AI Assistant Preview -->
		<section class="py-20 px-4 sm:px-6 lg:px-8 bg-muted/30">
			<div class="max-w-6xl mx-auto">
				<div class="grid lg:grid-cols-2 gap-12 items-center">
					<div class="animate-fade-in">
						<h2 class="text-3xl sm:text-4xl font-bold text-foreground mb-6">
							{$t('landing.aiTitle')}
						</h2>
						<p class="text-lg text-muted-foreground mb-6">
							{$t('landing.aiSubtitle')}
						</p>
						<ul class="space-y-4">
							<li class="flex items-start gap-3">
								<span class="text-primary text-xl">✓</span>
								<span class="text-foreground">{$t('landing.aiPoint1')}</span>
							</li>
							<li class="flex items-start gap-3">
								<span class="text-primary text-xl">✓</span>
								<span class="text-foreground">{$t('landing.aiPoint2')}</span>
							</li>
							<li class="flex items-start gap-3">
								<span class="text-primary text-xl">✓</span>
								<span class="text-foreground">{$t('landing.aiPoint3')}</span>
							</li>
							<li class="flex items-start gap-3">
								<span class="text-primary text-xl">✓</span>
								<span class="text-foreground">{$t('landing.aiPoint4')}</span>
							</li>
						</ul>
					</div>
					<div class="relative">
						<div class="bg-card rounded-2xl border border-border/50 shadow-xl overflow-hidden">
							<!-- Mock Chat Interface -->
							<div class="bg-secondary/30 px-4 py-3 border-b border-border/50">
								<span class="font-medium text-foreground">{$t('landing.aiAssistant')}</span>
							</div>
							<div class="p-4 space-y-4 min-h-[300px]">
								<div class="flex gap-3">
									<div class="w-8 h-8 rounded-full bg-primary/20 flex items-center justify-center text-sm">🤖</div>
									<div class="flex-1 bg-muted/50 rounded-lg p-3">
										<p class="text-sm text-foreground">{$t('landing.aiMockMessage1')}</p>
									</div>
								</div>
								<div class="flex gap-3 justify-end">
									<div class="bg-primary/10 rounded-lg p-3 max-w-[80%]">
										<p class="text-sm text-foreground">{$t('landing.aiMockReply')}</p>
									</div>
								</div>
								<div class="flex gap-3">
									<div class="w-8 h-8 rounded-full bg-primary/20 flex items-center justify-center text-sm">🤖</div>
									<div class="flex-1 bg-muted/50 rounded-lg p-3">
										<p class="text-sm text-foreground">{$t('landing.aiMockMessage2')}</p>
									</div>
								</div>
							</div>
						</div>
					</div>
				</div>
			</div>
		</section>

		<!-- CTA Section -->
		<section class="py-20 px-4 sm:px-6 lg:px-8">
			<div class="max-w-3xl mx-auto text-center">
				<h2 class="text-3xl sm:text-4xl font-bold text-foreground mb-6">
					{$t('landing.ctaTitle')}
				</h2>
				<p class="text-lg text-muted-foreground mb-8">
					{$t('landing.ctaSubtitle')}
				</p>
				<a
					href="/login"
					class="inline-block px-8 py-4 text-lg font-medium bg-primary text-primary-foreground rounded-xl hover:opacity-90 transition-all shadow-lg hover:shadow-xl"
				>
					{$t('landing.ctaButton')}
				</a>
				<p class="mt-4 text-sm text-muted-foreground">
					{$t('landing.ctaNote')}
				</p>
			</div>
		</section>

		<!-- Footer -->
		<Footer maxWidth="6xl" tagline={$t('footer.poweredByAi')} />
	</div>
{/if}
