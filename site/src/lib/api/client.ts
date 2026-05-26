import { writable } from 'svelte/store';

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

type AuthChangeCallback = () => void;

const tokenKey = 'diarum_auth_token';
const modelKey = 'diarum_auth_model';

function decodeJwtPayload(token: string): any | null {
    try {
        const payload = token.split('.')[1];
        if (!payload) return null;
        return JSON.parse(atob(payload.replace(/-/g, '+').replace(/_/g, '/')));
    } catch {
        return null;
    }
}

class AuthStore {
    token = '';
    model: User | null = null;
    private callbacks = new Set<AuthChangeCallback>();

    constructor() {
        if (typeof localStorage !== 'undefined') {
            this.token = localStorage.getItem(tokenKey) || '';
            const rawModel = localStorage.getItem(modelKey);
            try {
                this.model = rawModel ? JSON.parse(rawModel) : null;
            } catch {
                this.clear();
                return;
            }
            if (!this.isValid) {
                this.clear();
            }
        }
    }

    get isValid(): boolean {
        if (!this.token || !this.model) return false;
        const payload = decodeJwtPayload(this.token);
        if (!payload?.exp) return true;
        return payload.exp * 1000 > Date.now();
    }

    save(token: string, model: User) {
        this.token = token;
        this.model = model;
        if (typeof localStorage !== 'undefined') {
            localStorage.setItem(tokenKey, token);
            localStorage.setItem(modelKey, JSON.stringify(model));
        }
        this.notify();
    }

    clear() {
        this.token = '';
        this.model = null;
        if (typeof localStorage !== 'undefined') {
            localStorage.removeItem(tokenKey);
            localStorage.removeItem(modelKey);
        }
        this.notify();
    }

    onChange(callback: AuthChangeCallback) {
        this.callbacks.add(callback);
        return () => this.callbacks.delete(callback);
    }

    private notify() {
        for (const callback of this.callbacks) callback();
    }
}

export const pb = {
    authStore: new AuthStore()
};

pb.authStore.onChange(() => {
    currentUser.set(pb.authStore.model);
    isAuthenticated.set(pb.authStore.isValid);
});

export const currentUser = writable(pb.authStore.model);
export const isAuthenticated = writable(pb.authStore.isValid);

let restoreFetchInterceptor: (() => void) | null = null;

function getRequestUrl(input: RequestInfo | URL): URL | null {
    if (typeof window === 'undefined') return null;

    try {
        if (typeof input === 'string' || input instanceof URL) {
            return new URL(input.toString(), window.location.origin);
        }

        return new URL(input.url, window.location.origin);
    } catch {
        return null;
    }
}

function shouldHandleUnauthorized(input: RequestInfo | URL): boolean {
    const url = getRequestUrl(input);
    if (!url || url.origin !== window.location.origin) return false;
    if (!url.pathname.startsWith('/api/v1/')) return false;
    return !['/api/v1/auth/login', '/api/v1/auth/register'].includes(url.pathname);
}

export function installUnauthorizedApiHandler(onUnauthorized: () => void): () => void {
    if (typeof window === 'undefined') return () => { };
    if (restoreFetchInterceptor) return restoreFetchInterceptor;

    const originalFetch = window.fetch.bind(window);
    let handlingUnauthorized = false;

    window.fetch = async (input, init) => {
        const response = await originalFetch(input, init);

        if (response.status === 401 && shouldHandleUnauthorized(input) && !handlingUnauthorized) {
            handlingUnauthorized = true;
            pb.authStore.clear();
            onUnauthorized();
            queueMicrotask(() => {
                handlingUnauthorized = false;
            });
        }

        return response;
    };

    restoreFetchInterceptor = () => {
        window.fetch = originalFetch;
        restoreFetchInterceptor = null;
    };

    return restoreFetchInterceptor;
}
