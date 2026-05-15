<script lang="ts">
	import './app.css';
	import { onMount } from 'svelte';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { ScrollArea } from '$lib/components/ui/scroll-area/index.js';
	import { Separator } from '$lib/components/ui/separator/index.js';
	import {
		clearDumps,
		clearDumpsBefore,
		clearLog,
		clearMail,
		deleteDump,
		deleteMail,
		dumpList,
		logRaw,
		logSources,
		mailDetail,
		mailList,
		session,
		type DebugDump,
		type EventMessage,
		type LogSource,
		type MailDetail,
		type MailSummary,
		type View,
	} from '$lib/api';
	import {
		BadgeAlert,
		Inbox,
		Mail,
		MousePointerClick,
		Monitor,
		Moon,
		RefreshCw,
		ScrollText,
		Sun,
		Trash2,
	} from '@lucide/svelte';

	type ThemeMode = 'system' | 'light' | 'dark';
	type ContextMenuState = { kind: 'mail' | 'dump'; id: number; x: number; y: number } | null;

	let view = $state<View>(viewFromPath());
	let token = $state('');
	let status = $state('Connecting');
	let error = $state('');
	let sidebarCollapsed = $state(false);
	let themeMode = $state<ThemeMode>('system');
	let autoClearDumps = $state(false);
	let autoClearTimer: ReturnType<typeof setTimeout> | null = null;
	let autoClearPendingID: number | null = null;
	let contextMenu = $state<ContextMenuState>(null);

	let mails = $state<MailSummary[]>([]);
	let selectedMailID = $state<number | null>(null);
	let selectedMail = $state<MailDetail | null>(null);
	let mailBodyMode = $state<'html' | 'text' | 'raw'>('html');

	let dumps = $state<DebugDump[]>([]);
	let selectedDumpID = $state<number | null>(null);

	let logs = $state<LogSource[]>([]);
	let selectedLogID = $state('');
	let logText = $state('');
	let logFilter = $state('');
	let logLevel = $state('');
	let logOrder = $state<'desc' | 'asc'>('desc');

	let staleMail = $state(0);
	let staleDumps = $state(0);
	let staleLogs = $state(0);
	let acknowledgedDumpID = $state(0);

	const selectedDump = $derived(dumps.find((dump) => dump.id === selectedDumpID) ?? dumps[0] ?? null);
	const selectedLog = $derived(logs.find((log) => log.id === selectedLogID) ?? logs[0] ?? null);
	const visibleLogText = $derived(filteredLogText(logText, logFilter, logLevel, logOrder));

	onMount(() => {
		loadPreferences();
		const cleanupTheme = bindSystemTheme();
		void boot();
		const cleanup = connectEvents();
		window.addEventListener('popstate', handlePopState);
		window.addEventListener('click', closeContextMenu);
		window.addEventListener('keydown', handleKeydown);
		return () => {
			cleanup();
			cleanupTheme();
			window.removeEventListener('popstate', handlePopState);
			window.removeEventListener('click', closeContextMenu);
			window.removeEventListener('keydown', handleKeydown);
			if (autoClearTimer) clearTimeout(autoClearTimer);
		};
	});

	async function boot() {
		try {
			token = (await session()).token;
			await Promise.all([loadMail(), loadDumps(), loadLogs()]);
		} catch (err) {
			showError(err);
		}
	}

	function handlePopState() {
		view = viewFromPath();
		void refreshVisible();
	}

	function navigate(next: View, id?: number | string) {
		view = next;
		const suffix = id === undefined ? '' : `/${id}`;
		history.pushState(null, '', `/app/${next}${suffix}`);
		void refreshVisible();
	}

	async function refreshVisible() {
		if (view === 'mail') {
			await loadMail();
		} else if (view === 'dumps') {
			await loadDumps();
		} else {
			await loadLogs();
		}
	}

	async function loadMail() {
		mails = await mailList();
		staleMail = 0;
		const routeID = routeIDFromPath('mail');
		if (!mails.length) {
			selectedMailID = null;
			selectedMail = null;
			return;
		}
		if (routeID && mails.some((mail) => mail.id === routeID)) {
			selectedMailID = routeID;
		} else if (selectedMailID === null || !mails.some((mail) => mail.id === selectedMailID)) {
			selectedMailID = mails[0].id;
		}
		if (selectedMailID !== null) {
			await selectMail(selectedMailID, false);
		}
	}

	async function selectMail(id: number, push = true) {
		selectedMailID = id;
		selectedMail = await mailDetail(id);
		if (push) {
			history.pushState(null, '', `/app/mail/${id}`);
		}
	}

	async function loadDumps() {
		dumps = await dumpList();
		if (view === 'dumps' || acknowledgedDumpID === 0) {
			acknowledgedDumpID = newestDumpID();
			staleDumps = 0;
		} else {
			staleDumps = countNewDumps();
		}
		if (dumps.length && !dumps.some((dump) => dump.id === selectedDumpID)) {
			const routeID = routeIDFromPath('dumps');
			selectedDumpID = routeID ?? dumps[0].id;
		}
	}

	async function loadLogs() {
		logs = await logSources();
		staleLogs = 0;
		if (!logs.length) {
			selectedLogID = '';
			logText = '';
			return;
		}
		if (!selectedLogID || !logs.some((log) => log.id === selectedLogID)) {
			selectedLogID = logs[0].id;
		}
		logText = await logRaw(selectedLogID);
	}

	async function selectLog(id: string) {
		selectedLogID = id;
		logText = await logRaw(id);
	}

	async function clearCurrentMail() {
		if (!confirm('Clear captured mail?')) return;
		await clearMail(token);
		selectedMailID = null;
		selectedMail = null;
		await loadMail();
	}

	async function deleteCurrentMail(id: number) {
		closeContextMenu();
		await deleteMail(id, token);
		if (selectedMailID === id) {
			selectedMailID = null;
			selectedMail = null;
		}
		await loadMail();
	}

	async function clearCurrentDumps() {
		if (!confirm('Clear captured dumps?')) return;
		await clearDumps(token);
		selectedDumpID = null;
		await loadDumps();
	}

	async function deleteCurrentDump(id: number) {
		closeContextMenu();
		await deleteDump(id, token);
		if (selectedDumpID === id) {
			selectedDumpID = null;
		}
		await loadDumps();
	}

	async function clearCurrentLog() {
		if (!selectedLogID || !confirm('Clear this log?')) return;
		await clearLog(selectedLogID, token);
		await selectLog(selectedLogID);
	}

	function connectEvents() {
		let closed = false;
		let socket: WebSocket | null = null;

		const open = () => {
			if (closed) return;
			const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
			socket = new WebSocket(`${protocol}//${location.host}/api/events`);
			socket.onopen = () => (status = 'Live');
			socket.onclose = () => {
				status = 'Reconnecting';
				if (!closed) setTimeout(open, 1000);
			};
			socket.onerror = () => (status = 'Disconnected');
			socket.onmessage = (message) => {
				const event = JSON.parse(message.data) as EventMessage;
				void handleEvent(event);
			};
		};

		open();
		return () => {
			closed = true;
			socket?.close();
		};
	}

	async function handleEvent(event: EventMessage) {
		if (event.type === 'mail.created' || event.type === 'mail.cleared') {
			if (view === 'mail') await loadMail();
			else staleMail += 1;
		}
		if (event.type === 'dump.created' || event.type === 'debug.cleared') {
			if (event.type === 'dump.created' && autoClearDumps && event.id) {
				scheduleAutoClear(Number(event.id));
			} else {
				await loadDumps();
			}
		}
		if (event.type === 'log.changed') {
			if (view === 'logs') await loadLogs();
			else staleLogs += 1;
		}
	}

	function showError(err: unknown) {
		error = err instanceof Error ? err.message : String(err);
	}

	function openContextMenu(event: MouseEvent, kind: 'mail' | 'dump', id: number) {
		event.preventDefault();
		contextMenu = {
			kind,
			id,
			x: Math.min(event.clientX, window.innerWidth - 190),
			y: Math.min(event.clientY, window.innerHeight - 74),
		};
	}

	function closeContextMenu() {
		contextMenu = null;
	}

	function handleKeydown(event: KeyboardEvent) {
		if (event.key === 'Escape') closeContextMenu();
	}

	function deleteContextItem() {
		const item = contextMenu;
		if (!item) return;
		if (item.kind === 'mail') {
			void deleteCurrentMail(item.id);
		} else {
			void deleteCurrentDump(item.id);
		}
	}

	function scheduleAutoClear(id: number) {
		if (!Number.isFinite(id) || id <= 0) return;
		if (autoClearPendingID === null || id < autoClearPendingID) {
			autoClearPendingID = id;
		}
		if (autoClearTimer) clearTimeout(autoClearTimer);
		autoClearTimer = setTimeout(async () => {
			const targetID = autoClearPendingID;
			autoClearTimer = null;
			autoClearPendingID = null;
			try {
				if (targetID !== null) {
					await clearDumpsBefore(targetID, token);
				}
				await loadDumps();
			} catch (err) {
				showError(err);
			}
		}, 250);
	}

	function newestDumpID() {
		return dumps.reduce((max, dump) => Math.max(max, dump.id), 0);
	}

	function countNewDumps() {
		return dumps.filter((dump) => dump.id > acknowledgedDumpID).length;
	}

	function loadPreferences() {
		sidebarCollapsed = localStorage.getItem('herdlite.sidebar') === 'collapsed';
		autoClearDumps = localStorage.getItem('herdlite.dumps.auto-clear') === 'true';
		const storedTheme = localStorage.getItem('herdlite.theme');
		if (storedTheme === 'light' || storedTheme === 'dark' || storedTheme === 'system') {
			themeMode = storedTheme;
		}
		applyTheme();
	}

	function bindSystemTheme() {
		const media = window.matchMedia('(prefers-color-scheme: dark)');
		const update = () => {
			if (themeMode === 'system') applyTheme();
		};
		media.addEventListener('change', update);
		return () => media.removeEventListener('change', update);
	}

	function setSidebarCollapsed(next: boolean) {
		sidebarCollapsed = next;
		localStorage.setItem('herdlite.sidebar', next ? 'collapsed' : 'expanded');
	}

	function setThemeMode(next: ThemeMode) {
		themeMode = next;
		localStorage.setItem('herdlite.theme', next);
		applyTheme();
	}

	function applyTheme() {
		const dark =
			themeMode === 'dark' ||
			(themeMode === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches);
		document.documentElement.classList.toggle('dark', dark);
		document.documentElement.classList.toggle('light', !dark);
		document.documentElement.style.colorScheme = dark ? 'dark' : 'light';
	}

	function setAutoClearDumps(next: boolean) {
		autoClearDumps = next;
		localStorage.setItem('herdlite.dumps.auto-clear', String(next));
	}

	function viewFromPath(): View {
		const part = location.pathname.replace(/^\/app\/?/, '').split('/')[0];
		if (part === 'dumps' || part === 'logs' || part === 'mail') return part;
		return 'mail';
	}

	function routeIDFromPath(kind: View) {
		const parts = location.pathname.replace(/^\/app\/?/, '').split('/');
		if (parts[0] !== kind) return null;
		const id = Number(parts[1]);
		return Number.isFinite(id) && id > 0 ? id : null;
	}

	function filteredLogText(text: string, filter: string, level: string, order: 'desc' | 'asc') {
		let lines = text.split(/\r?\n/);
		if (filter) lines = lines.filter((line) => line.toLowerCase().includes(filter.toLowerCase()));
		if (level) lines = lines.filter((line) => line.toLowerCase().includes(level));
		if (order === 'desc') lines = lines.reverse();
		return lines.join('\n');
	}
</script>

<main class="debug-app" class:collapsed={sidebarCollapsed}>
	<aside
		class="sidebar"
		title={sidebarCollapsed ? 'Click empty sidebar space to expand' : 'Click empty sidebar space to collapse'}
	>
		<button
			class="sidebar-toggle-zone"
			aria-label={sidebarCollapsed ? 'Expand sidebar' : 'Collapse sidebar'}
			onclick={() => setSidebarCollapsed(!sidebarCollapsed)}
		></button>
		<div class="brand">
			<div class="brand-mark">
				<picture>
					<source srcset="/app/logo-dark.svg" media="(prefers-color-scheme: dark)" />
					<img src="/app/logo.svg" alt="" />
				</picture>
			</div>
			<div class="brand-copy">
				<strong>Herdlite</strong>
				<span>{status}</span>
			</div>
		</div>

		<nav>
			<button class:active={view === 'mail'} aria-label="Mail" title="Mail" onclick={() => navigate('mail')}>
				<Mail size={16} />
				<span class="nav-label">Mail</span>
				{#if staleMail}<Badge class="nav-badge" variant="secondary">{staleMail}</Badge>{/if}
			</button>
			<button class:active={view === 'dumps'} aria-label="Dumps" title="Dumps" onclick={() => navigate('dumps')}>
				<BadgeAlert size={16} />
				<span class="nav-label">Dumps</span>
				{#if staleDumps}<Badge class="nav-badge" variant="secondary">{staleDumps}</Badge>{/if}
			</button>
			<button class:active={view === 'logs'} aria-label="Logs" title="Logs" onclick={() => navigate('logs')}>
				<ScrollText size={16} />
				<span class="nav-label">Logs</span>
				{#if staleLogs}<Badge class="nav-badge" variant="secondary">{staleLogs}</Badge>{/if}
			</button>
		</nav>
	</aside>

	<section class="workspace">
		<header class="topbar">
			<div>
				<h1>{view === 'mail' ? 'Mail' : view === 'dumps' ? 'Dumps' : 'Logs'}</h1>
				<p>
					{view === 'mail'
						? `${mails.length} captured messages`
						: view === 'dumps'
							? `${dumps.length} captured dumps`
							: `${logs.length} log sources`}
				</p>
			</div>
			<div class="actions">
				<div class="theme-toggle" aria-label="Theme mode">
					<button class:active={themeMode === 'system'} title="System theme" onclick={() => setThemeMode('system')}>
						<Monitor size={14} />
					</button>
					<button class:active={themeMode === 'light'} title="Light theme" onclick={() => setThemeMode('light')}>
						<Sun size={14} />
					</button>
					<button class:active={themeMode === 'dark'} title="Dark theme" onclick={() => setThemeMode('dark')}>
						<Moon size={14} />
					</button>
				</div>
				{#if view === 'dumps'}
					<button
						class="switch-button"
						class:active={autoClearDumps}
						role="switch"
						aria-checked={autoClearDumps}
						onclick={() => setAutoClearDumps(!autoClearDumps)}
					>
						<span></span>
						Auto clear
					</button>
				{/if}
				<Button variant="outline" size="sm" onclick={refreshVisible}><RefreshCw />Refresh</Button>
				{#if view === 'mail'}
					<Button variant="destructive" size="sm" onclick={clearCurrentMail}><Trash2 />Clear</Button>
				{:else if view === 'dumps'}
					<Button variant="destructive" size="sm" onclick={clearCurrentDumps}><Trash2 />Clear</Button>
				{:else}
					<Button variant="destructive" size="sm" onclick={clearCurrentLog}><Trash2 />Clear</Button>
				{/if}
			</div>
		</header>

		{#if error}
			<div class="error">{error}</div>
		{/if}

		{#if view === 'mail'}
			<div class="split">
				<ScrollArea class="list-pane">
					{#each mails as message}
						<button
							class:item-active={message.id === selectedMailID}
							class="item"
							onclick={() => selectMail(message.id)}
							oncontextmenu={(event) => openContextMenu(event, 'mail', message.id)}
						>
							<strong>{message.subject || '(No subject)'}</strong>
							<span>{message.sender}</span>
							<small>{message.project_name} · {message.received_at}</small>
							{#if message.attachment_count}<Badge variant="outline">{message.attachment_count} attachments</Badge>{/if}
						</button>
					{:else}
						<div class="empty"><Inbox />No captured mail.</div>
					{/each}
				</ScrollArea>

				<section class="detail-pane">
					{#if selectedMail}
						<div class="detail-header">
							<h2>{selectedMail.subject || '(No subject)'}</h2>
							<p>{selectedMail.sender} → {selectedMail.recipients}</p>
						</div>
						<div class="mode-row">
							<Button variant={mailBodyMode === 'html' ? 'default' : 'outline'} size="xs" onclick={() => (mailBodyMode = 'html')}>HTML</Button>
							<Button variant={mailBodyMode === 'text' ? 'default' : 'outline'} size="xs" onclick={() => (mailBodyMode = 'text')}>Text</Button>
							<Button variant={mailBodyMode === 'raw' ? 'default' : 'outline'} size="xs" onclick={() => (mailBodyMode = 'raw')}>Raw</Button>
						</div>
						<Separator />
						{#if selectedMail.attachments.length}
							<div class="attachments">
								{#each selectedMail.attachments as attachment}
									<a href={attachment.url}>{attachment.filename || `attachment-${attachment.id}`}</a>
								{/each}
							</div>
						{/if}
						<ScrollArea class="content-pane">
							{#if mailBodyMode === 'html'}
								<div class="mail-html">{@html selectedMail.html_body || `<pre>${selectedMail.text_body}</pre>`}</div>
							{:else if mailBodyMode === 'text'}
								<pre>{selectedMail.text_body || 'No text body.'}</pre>
							{:else}
								<pre>{selectedMail.raw_mime || 'No raw MIME stored.'}</pre>
							{/if}
						</ScrollArea>
					{:else}
						<div class="empty"><Inbox />Select a message.</div>
					{/if}
				</section>
			</div>
		{:else if view === 'dumps'}
			<div class="split">
				<ScrollArea class="list-pane">
					{#each dumps as dump}
						<button
							class:item-active={dump.id === selectedDump?.id}
							class="item"
							onclick={() => (selectedDumpID = dump.id)}
							oncontextmenu={(event) => openContextMenu(event, 'dump', dump.id)}
						>
							<strong>{dump.project_name || 'Unknown Project'}</strong>
							<span>{dump.file || dump.uri || dump.command || dump.sapi}</span>
							<small>{dump.captured_at}</small>
						</button>
					{:else}
						<div class="empty"><BadgeAlert />No captured dumps.</div>
					{/each}
				</ScrollArea>
				<section class="detail-pane">
					{#if selectedDump}
						<div class="detail-header">
							<h2>{selectedDump.project_name}</h2>
							<p>{selectedDump.sapi} · {selectedDump.file || selectedDump.uri || selectedDump.command}</p>
						</div>
						<Separator />
						<ScrollArea class="content-pane dump-render">
							{@html selectedDump.html}
						</ScrollArea>
					{:else}
						<div class="empty"><BadgeAlert />Waiting for dumps.</div>
					{/if}
				</section>
			</div>
		{:else}
			<div class="split">
				<ScrollArea class="list-pane">
					{#each logs as log}
						<button class:item-active={log.id === selectedLogID} class="item" onclick={() => selectLog(log.id)}>
							<strong>{log.label}</strong>
							<small>{log.id}</small>
						</button>
					{/each}
				</ScrollArea>
				<section class="detail-pane">
					<div class="detail-header">
						<h2>{selectedLog?.label ?? 'Logs'}</h2>
						<div class="log-controls">
							<Input placeholder="Filter" bind:value={logFilter} />
							<select bind:value={logLevel}>
								<option value="">All</option>
								<option value="error">error</option>
								<option value="warn">warn</option>
								<option value="info">info</option>
								<option value="debug">debug</option>
							</select>
							<select bind:value={logOrder}>
								<option value="desc">Newest</option>
								<option value="asc">Oldest</option>
							</select>
						</div>
					</div>
					<Separator />
					<ScrollArea class="content-pane log-output">
						<pre>{visibleLogText || 'No log lines.'}</pre>
					</ScrollArea>
				</section>
			</div>
		{/if}
	</section>

	{#if contextMenu}
		<div class="context-menu" style={`left:${contextMenu.x}px;top:${contextMenu.y}px`} role="menu">
			<button role="menuitem" onclick={deleteContextItem}>
				<Trash2 size={14} />
				Delete {contextMenu.kind === 'mail' ? 'mail' : 'dump'}
			</button>
			<div><MousePointerClick size={13} />Right-click actions</div>
		</div>
	{/if}
</main>
