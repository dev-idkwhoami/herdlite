export type View = 'mail' | 'dumps' | 'logs';

export type MailSummary = {
	id: number;
	project_name: string;
	sender: string;
	reply_to: string;
	recipients: string;
	subject: string;
	received_at: string;
	attachment_count: number;
	has_html: boolean;
	has_text: boolean;
};

export type MailDetail = MailSummary & {
	text_body: string;
	html_body: string;
	raw_mime: string;
	attachments: MailAttachment[];
};

export type MailAttachment = {
	id: number;
	filename: string;
	content_type: string;
	size: number;
	url: string;
};

export type DebugDump = {
	id: number;
	project_name: string;
	project_path: string;
	sapi: string;
	uri: string;
	command: string;
	file: string;
	html: string;
	captured_at: string;
};

export type LogSource = {
	id: string;
	label: string;
};

export type EventMessage = {
	type: string;
	id?: string;
};

async function request<T>(url: string, init?: RequestInit): Promise<T> {
	const response = await fetch(url, init);
	if (!response.ok) {
		throw new Error(`${response.status} ${response.statusText}`);
	}
	return response.json() as Promise<T>;
}

export function session() {
	return request<{ token: string }>('/api/session');
}

export function mailList() {
	return request<MailSummary[]>('/api/mail');
}

export function mailDetail(id: number) {
	return request<MailDetail>(`/api/mail/${id}`);
}

export function clearMail(token: string) {
	return request<{ ok: boolean; count: number }>('/api/mail/clear', {
		method: 'POST',
		headers: { 'X-Herdlite-Token': token },
	});
}

export function deleteMail(id: number, token: string) {
	return request<{ ok: boolean; count: number }>(`/api/mail/${id}`, {
		method: 'DELETE',
		headers: { 'X-Herdlite-Token': token },
	});
}

export function dumpList() {
	return request<DebugDump[]>('/api/dumps');
}

export function clearDumps(token: string) {
	return request<{ ok: boolean }>('/api/dumps/clear', {
		method: 'POST',
		headers: { 'X-Herdlite-Token': token },
	});
}

export function deleteDump(id: number, token: string) {
	return request<{ ok: boolean; count: number }>(`/api/dumps/${id}`, {
		method: 'DELETE',
		headers: { 'X-Herdlite-Token': token },
	});
}

export function clearDumpsBefore(id: number, token: string) {
	return request<{ ok: boolean; count: number }>(`/api/dumps/clear-before/${id}`, {
		method: 'POST',
		headers: { 'X-Herdlite-Token': token },
	});
}

export function logSources() {
	return request<LogSource[]>('/api/logs');
}

export async function logRaw(id: string) {
	const response = await fetch(`/logs/${id}/raw`);
	if (!response.ok) {
		throw new Error(`${response.status} ${response.statusText}`);
	}
	return response.text();
}

export function clearLog(id: string, token: string) {
	return request<{ ok: boolean }>(`/logs/${id}/clear`, {
		method: 'POST',
		headers: { 'X-Herdlite-Token': token },
	});
}
