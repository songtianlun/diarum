// PWA utility functions
import { writable } from 'svelte/store';
import { get } from 'svelte/store';

interface BeforeInstallPromptEvent extends Event {
	prompt(): Promise<void>;
	userChoice: Promise<{ outcome: 'accepted' | 'dismissed' }>;
}

// Store for installation prompt
export const deferredPrompt = writable<BeforeInstallPromptEvent | null>(null);
export const canInstall = writable(false);
export const isUpdateAvailable = writable(false);

// Initialize PWA features
export function initPWA() {
	if (typeof window === 'undefined') return;

	// Listen for beforeinstallprompt event
	window.addEventListener('beforeinstallprompt', (e) => {
		e.preventDefault();
		deferredPrompt.set(e as BeforeInstallPromptEvent);
		canInstall.set(true);
	});

	// Listen for app installed event
	window.addEventListener('appinstalled', () => {
		deferredPrompt.set(null);
		canInstall.set(false);
		console.log('PWA installed successfully');
	});

	// Check if app is already installed
	if (window.matchMedia('(display-mode: standalone)').matches) {
		canInstall.set(false);
	}
}

// Trigger installation prompt
export async function installPWA() {
	const prompt = get(deferredPrompt);

	if (!prompt) {
		console.log('Installation prompt not available');
		return false;
	}

	prompt.prompt();
	const { outcome } = await prompt.userChoice;

	if (outcome === 'accepted') {
		console.log('User accepted the install prompt');
		deferredPrompt.set(null);
		canInstall.set(false);
		return true;
	} else {
		console.log('User dismissed the install prompt');
		return false;
	}
}

// Check for service worker updates
export function checkForUpdates() {
	if (typeof window === 'undefined' || !('serviceWorker' in navigator)) return;

	navigator.serviceWorker.ready.then((registration) => {
		registration.update();
	});
}

// Listen for service worker updates
export function listenForUpdates() {
	if (typeof window === 'undefined' || !('serviceWorker' in navigator)) return;

	navigator.serviceWorker.addEventListener('controllerchange', () => {
		isUpdateAvailable.set(true);
	});

	// Check for updates every 60 minutes
	setInterval(() => {
		checkForUpdates();
	}, 60 * 60 * 1000);
}

// Reload to apply updates
export function applyUpdate() {
	window.location.reload();
}
