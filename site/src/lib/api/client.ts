import PocketBase from 'pocketbase';
import { writable } from 'svelte/store';

// Create PocketBase instance
export const pb = new PocketBase('/');

// Auto-refresh authentication
pb.authStore.onChange(() => {
	currentUser.set(pb.authStore.model);
	isAuthenticated.set(pb.authStore.isValid);
});

// Auth stores
export const currentUser = writable(pb.authStore.model);
export const isAuthenticated = writable(pb.authStore.isValid);

// Types
export interface User {
	id: string;
	email: string;
	username: string;
	created: string;
	updated: string;
}

export interface Diary {
	id?: string;
	date: string;
	content: string;
	mood?: string;
	weather?: string;
	owner: string;
	created?: string;
	updated?: string;
}

export interface Media {
	id?: string;
	file: string;
	name?: string;
	alt?: string;
	diary?: string[];
	owner: string;
	created?: string;
	updated?: string;
}

export interface UploadProgress {
	loaded: number;
	total: number;
	percentage: number;
}
