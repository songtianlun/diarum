import { pb } from './client';

export type ChatScope = 'all' | 'this_month' | 'this_year' | 'none';

export interface Conversation {
	id: string;
	title: string;
	created: string;
	updated: string;
	message_count?: number;
}

export interface DiaryReference {
	id: string;
	date: string;
	mood?: string;
	weather?: string;
	preview?: string;
}

export interface ToolActivity {
	id: string;
	name: string;
	args?: Record<string, unknown>;
	status: 'running' | 'done' | 'error';
	count?: number;
	dates?: string[];
	error?: string;
}

export interface Message {
	id: string;
	role: 'user' | 'assistant';
	content: string;
	thinking?: string;
	thinkingDurationSec?: number;
	tools?: ToolActivity[];
	referenced_diaries?: string[];
	references?: DiaryReference[];
	created: string;
}

export interface ConversationDetail {
	conversation: Conversation;
	messages: Message[];
}

export interface StreamToolPayload {
	id: string;
	name: string;
	args?: Record<string, unknown>;
	count?: number;
	dates?: string[];
	error?: string;
}

export interface StreamChunk {
	type?: 'thinking' | 'tool_start' | 'tool_end' | 'content' | 'references' | 'title' | 'done' | 'error';
	content?: string;
	thinking?: string;
	tool?: StreamToolPayload;
	references?: DiaryReference[];
	done?: boolean;
	message_id?: string;
	user_message_id?: string;
	referenced_diaries?: string[];
	error?: string;
	title?: string;
}

export interface StreamChatOptions {
	signal?: AbortSignal;
	scope?: ChatScope;
	regenerate?: boolean;
}

function authHeaders(json = false): Record<string, string> {
	const headers: Record<string, string> = {
		Authorization: `Bearer ${pb.authStore.token}`
	};
	if (json) {
		headers['Content-Type'] = 'application/json';
	}
	return headers;
}

export async function getConversations(): Promise<Conversation[]> {
	const response = await fetch('/api/v1/ai/conversations', {
		headers: authHeaders()
	});

	if (!response.ok) {
		throw new Error('Failed to get conversations');
	}

	return await response.json();
}

export async function createConversation(title?: string): Promise<Conversation> {
	const response = await fetch('/api/v1/ai/conversations', {
		method: 'POST',
		headers: authHeaders(true),
		body: JSON.stringify({ title: title || '' })
	});

	if (!response.ok) {
		throw new Error('Failed to create conversation');
	}

	return await response.json();
}

export async function getConversation(id: string): Promise<ConversationDetail> {
	const response = await fetch(`/api/v1/ai/conversations/${encodeURIComponent(id)}`, {
		headers: authHeaders()
	});

	if (!response.ok) {
		throw new Error('Failed to get conversation');
	}

	return await response.json();
}

export async function deleteConversation(id: string): Promise<void> {
	const response = await fetch(`/api/v1/ai/conversations/${encodeURIComponent(id)}`, {
		method: 'DELETE',
		headers: authHeaders()
	});

	if (!response.ok) {
		throw new Error('Failed to delete conversation');
	}
}

export async function updateConversationTitle(id: string, title: string): Promise<Conversation> {
	const response = await fetch(`/api/v1/ai/conversations/${encodeURIComponent(id)}`, {
		method: 'PUT',
		headers: authHeaders(true),
		body: JSON.stringify({ title })
	});

	if (!response.ok) {
		throw new Error('Failed to update conversation');
	}

	return await response.json();
}

export async function generateConversationTitle(id: string): Promise<Conversation | null> {
	try {
		const response = await fetch(`/api/v1/ai/conversations/${encodeURIComponent(id)}/title`, {
			method: 'POST',
			headers: authHeaders()
		});
		if (!response.ok) return null;
		return await response.json();
	} catch {
		return null;
	}
}

export async function deleteMessagesFrom(conversationId: string, messageId: string): Promise<void> {
	const response = await fetch(
		`/api/v1/ai/conversations/${encodeURIComponent(conversationId)}/messages/${encodeURIComponent(messageId)}`,
		{
			method: 'DELETE',
			headers: authHeaders()
		}
	);
	if (!response.ok) {
		throw new Error('Failed to delete messages');
	}
}

export async function* streamChat(
	conversationId: string,
	content: string,
	options: StreamChatOptions = {}
): AsyncGenerator<StreamChunk> {
	const response = await fetch('/api/v1/ai/chat', {
		method: 'POST',
		headers: authHeaders(true),
		signal: options.signal,
		body: JSON.stringify({
			conversation_id: conversationId,
			content,
			scope: options.scope ?? 'all',
			regenerate: Boolean(options.regenerate)
		})
	});

	if (!response.ok) {
		let errMessage = 'Failed to send message';
		try {
			const errBody = await response.json();
			if (errBody?.message) errMessage = errBody.message;
		} catch {
			// Ignore JSON parse errors
		}
		throw new Error(errMessage);
	}

	if (!response.body) {
		throw new Error('No response body');
	}

	const reader = response.body.getReader();
	const decoder = new TextDecoder();
	let buffer = '';

	try {
		while (true) {
			const { done, value } = await reader.read();
			if (done) break;

			buffer += decoder.decode(value, { stream: true });
			const lines = buffer.split('\n');
			buffer = lines.pop() || '';

			for (const rawLine of lines) {
				const line = rawLine.trimEnd();
				if (line.startsWith('data:')) {
					const payload = line.slice(5).trim();
					if (!payload || payload === '[DONE]') continue;
					try {
						yield JSON.parse(payload) as StreamChunk;
					} catch {
						// Skip malformed chunk
					}
				}
			}
		}
	} finally {
		reader.releaseLock();
	}
}
