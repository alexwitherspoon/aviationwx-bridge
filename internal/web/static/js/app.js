/**
 * AviationWX.org Bridge - Web Console
 * Frontend JavaScript application
 */

// State
let config = null;
let status = null;
let previousCameraIds = null;
let cameras = [];
let stations = [];
let timeUpdateInterval = null;
/** Unsaved local edits — skip syncing those form sections from the server until Save. */
let timezoneDirty = false;
let webConsoleDirty = false;
let uploadSettingsDirty = false;
let apiLinkDirty = false;
/** Camera add/edit modal: user changed a field since the form was shown. */
let cameraFormDirty = false;
/** Aborts an in-flight Discover SSE when Discover is clicked again. */
let discoverAbort = null;

function shouldSkipHydratingSettingsForms() {
    const fn = window.shouldSkipSettingsHydrate;
    if (typeof fn === 'function') {
        return fn({ timezoneDirty, webConsoleDirty, uploadSettingsDirty, apiLinkDirty });
    }
    return timezoneDirty || webConsoleDirty || uploadSettingsDirty || apiLinkDirty;
}

function shouldWarnBeforeUnload() {
    const fn = window.shouldWarnBeforePageLeave;
    if (typeof fn === 'function') {
        return fn({ timezoneDirty, webConsoleDirty, uploadSettingsDirty, apiLinkDirty, cameraFormDirty });
    }
    return timezoneDirty || webConsoleDirty || uploadSettingsDirty || apiLinkDirty || cameraFormDirty;
}

// Timezone list (IANA timezones for US and common international)
const TIMEZONES = [
    { value: 'America/New_York', label: 'Eastern Time (New York)' },
    { value: 'America/Chicago', label: 'Central Time (Chicago)' },
    { value: 'America/Denver', label: 'Mountain Time (Denver)' },
    { value: 'America/Phoenix', label: 'Arizona (Phoenix)' },
    { value: 'America/Los_Angeles', label: 'Pacific Time (Los Angeles)' },
    { value: 'America/Anchorage', label: 'Alaska (Anchorage)' },
    { value: 'Pacific/Honolulu', label: 'Hawaii (Honolulu)' },
    { value: 'UTC', label: 'UTC' },
    { value: 'Europe/London', label: 'London' },
    { value: 'Europe/Paris', label: 'Paris' },
    { value: 'Europe/Berlin', label: 'Berlin' },
    { value: 'Asia/Tokyo', label: 'Tokyo' },
    { value: 'Asia/Shanghai', label: 'Shanghai' },
    { value: 'Australia/Sydney', label: 'Sydney' },
];

/** Default web console listen port (matches bridge default when unset on disk). */
const DEFAULT_WEB_CONSOLE_PORT = 1229;

const WEB_AUTH_STORAGE_KEY = 'aviationwx_web_password';

function getWebPassword() {
    return sessionStorage.getItem(WEB_AUTH_STORAGE_KEY) || '';
}

function setWebPassword(password) {
    sessionStorage.setItem(WEB_AUTH_STORAGE_KEY, String(password));
}

function clearWebPassword() {
    sessionStorage.removeItem(WEB_AUTH_STORAGE_KEY);
}

function authHeaders() {
    const password = getWebPassword();
    if (!password) {
        return {};
    }
    const encode = window.encodeBasicAuthHeader;
    if (!encode) {
        return {};
    }
    return { Authorization: encode('admin', password) };
}

async function promptForWebPassword() {
    const entered = window.prompt('Web console password:', 'aviationwx');
    if (entered === null) {
        return false;
    }
    setWebPassword(entered);
    return true;
}

async function ensureAuth() {
    if (getWebPassword()) {
        return;
    }
    await promptForWebPassword();
}

/** fetch with Basic Auth; on 401 clears cached password, re-prompts, and retries once. */
async function fetchWithAuth(url, options = {}, retried = false) {
    const response = await fetch(url, {
        ...options,
        credentials: 'same-origin',
        headers: {
            ...options.headers,
            ...authHeaders(),
        },
    });
    if (response.status === 401 && !retried) {
        clearWebPassword();
        if (await promptForWebPassword()) {
            return fetchWithAuth(url, options, true);
        }
    }
    return response;
}

/** Object URLs for camera preview <img> tags (Authorization cannot be sent on img src). */
const previewBlobURLs = new Map();

function revokePreviewBlob(cameraId) {
    const old = previewBlobURLs.get(cameraId);
    if (old) {
        URL.revokeObjectURL(old);
        previewBlobURLs.delete(cameraId);
    }
}

function applyPreviewBlob(cameraId, objectURL) {
    revokePreviewBlob(cameraId);
    previewBlobURLs.set(cameraId, objectURL);
    document.querySelectorAll(`.camera-preview-img[data-camera-id="${cameraId}"]`).forEach((img) => {
        img.src = objectURL;
        img.style.display = '';
        const placeholder = img.nextElementSibling;
        if (placeholder) {
            placeholder.style.display = 'none';
        }
    });
}

function showPreviewUnavailable(cameraId) {
    document.querySelectorAll(`.camera-preview-img[data-camera-id="${cameraId}"]`).forEach((img) => {
        img.removeAttribute('src');
        img.style.display = 'none';
        const placeholder = img.nextElementSibling;
        if (placeholder) {
            placeholder.style.display = 'flex';
        }
    });
}

/** Load preview via authenticated fetch so the browser never prompts Basic Auth for <img>. */
async function loadAuthenticatedPreview(cameraId) {
    try {
        const response = await fetchWithAuth(`/api/cameras/${cameraId}/preview?t=${Date.now()}`);
        if (!response.ok) {
            showPreviewUnavailable(cameraId);
            return;
        }
        const blob = await response.blob();
        applyPreviewBlob(cameraId, URL.createObjectURL(blob));
    } catch (err) {
        console.log(`Failed to load preview for ${cameraId}:`, err);
        showPreviewUnavailable(cameraId);
    }
}

function hydratePreviewImages() {
    const cameraIds = new Set();
    document.querySelectorAll('.camera-preview-img[data-camera-id]').forEach((img) => {
        const id = img.dataset.cameraId;
        if (id) cameraIds.add(id);
    });
    cameraIds.forEach((cameraId) => {
        loadAuthenticatedPreview(cameraId);
    });
}

// Initialize application
document.addEventListener('DOMContentLoaded', async () => {
    setupNavigation();
    populateTimezones();
    await ensureAuth();
    await refreshStatus();
    await loadConfig();
    await loadCameras();
    await loadStations();
    startTimeUpdates();
    startAutoRefresh(); // Auto-refresh dashboard every second
    startLiveLogs();    // Start live log streaming
    
    // Check for first run (dismissible welcome)
    updateSetupBanner();
    renderConfigBanners();

    document.addEventListener('input', markCameraFormDirtyIfNeeded, true);
    document.addEventListener('change', markCameraFormDirtyIfNeeded, true);

    window.addEventListener('beforeunload', (e) => {
        if (shouldWarnBeforeUnload()) {
            e.preventDefault();
            e.returnValue = '';
        }
    });
});

// Navigation
function setupNavigation() {
    const links = document.querySelectorAll('.nav-link');
    links.forEach(link => {
        link.addEventListener('click', (e) => {
            e.preventDefault();
            const section = link.dataset.section;
            showSection(section);
        });
    });
    
    // Handle hash navigation
    if (window.location.hash) {
        const section = window.location.hash.slice(1);
        showSection(section);
    }
}

function showSection(sectionId) {
    // Update nav
    document.querySelectorAll('.nav-link').forEach(link => {
        link.classList.toggle('active', link.dataset.section === sectionId);
    });
    
    // Update sections
    document.querySelectorAll('.section').forEach(section => {
        section.classList.toggle('active', section.id === sectionId);
    });
    
    // Load section-specific data
    if (sectionId === 'settings') {
        loadGlobalSettings();
        loadAPILinkSettings();
        loadUploadSshKeys();
    }
    if (sectionId === 'weather') {
        loadStations();
        updateStationsDisplay();
    }
    
    // Update URL
    window.location.hash = sectionId;
}

// API calls
async function api(endpoint, options = {}) {
    const url = `/api${endpoint}`;
    const response = await fetchWithAuth(url, {
        ...options,
        headers: {
            'Content-Type': 'application/json',
            ...options.headers,
        },
    });

    if (!response.ok) {
        const error = await response.text();
        try {
            const parsed = JSON.parse(error);
            if (parsed && parsed.error) {
                throw new Error(parsed.error);
            }
        } catch (e) {
            if (e instanceof Error && e.message && !e.message.startsWith('{') && e.message !== error) {
                throw e;
            }
        }
        throw new Error(error || `HTTP ${response.status}`);
    }

    return response.json();
}

/** @returns {Promise<boolean>} true when status was loaded */
async function refreshStatus() {
    try {
        status = await api('/status');
        updateStatusDisplay();
        const dot = document.getElementById('statusDot');
        dot.classList.remove('error');
        dot.classList.add('connected');
        document.getElementById('statusText').textContent = 'Connected';
        updateSetupBanner();
        renderConfigBanners();
        return true;
    } catch (err) {
        console.error('Failed to fetch status:', err);
        const dot = document.getElementById('statusDot');
        dot.classList.remove('connected');
        dot.classList.add('error');
        document.getElementById('statusText').textContent = 'Disconnected';
        return false;
    }
}

/** Loads global config from the API (required for settings UI and form defaults). */
async function loadConfig() {
    try {
        config = await api('/config');
        // Hydrate sections independently so one dirty form does not block others.
        if (!timezoneDirty && !webConsoleDirty && !uploadSettingsDirty) {
            loadGlobalSettings();
        } else {
            updateSettingsUnsavedHints();
        }
        loadAPILinkSettings();
        renderConfigBanners();
    } catch (err) {
        console.error('Failed to load config:', err);
    }
}

async function loadCameras() {
    try {
        cameras = await api('/cameras');
        const cameraIds = cameras.map((c) => c.id).sort().join(',');
        const camerasChanged = previousCameraIds !== cameraIds;
        previousCameraIds = cameraIds;

        if (camerasChanged) {
            updateCameraList();
            updateCameraOverview();
            renderConfigBanners();
        } else {
            updateCameraListStatus();
            updateCameraOverviewStatus();
        }
    } catch (err) {
        console.error('Failed to load cameras:', err);
    }
}

async function loadStations() {
    try {
        stations = await api('/stations');
        updateStationsDisplay();
        renderConfigBanners();
    } catch (err) {
        console.error('Failed to load stations:', err);
    }
}

// Status display
function updateStatusDisplay() {
    if (!status) return;
    
    // Update version display
    if (status.version) {
        const versionEl = document.getElementById('appVersion');
        const updateChannel = status.update_channel || 'latest';
        const channelBadge = updateChannel === 'edge' 
            ? '<span style="background: #f59e0b; color: #000; padding: 2px 6px; border-radius: 4px; font-size: 0.75em; margin-left: 4px;">EDGE</span>'
            : '<span style="background: #10b981; color: #fff; padding: 2px 6px; border-radius: 4px; font-size: 0.75em; margin-left: 4px;">LATEST</span>';
        versionEl.innerHTML = `v${status.version} ${channelBadge}`;
        const localTag = status.configured_image_tag || status.last_known_good;
        if (localTag && localTag !== status.version) {
            const tagNote = document.createElement('span');
            tagNote.style.opacity = '0.85';
            tagNote.style.fontSize = '0.85em';
            tagNote.textContent = ` (tag: ${localTag})`;
            versionEl.appendChild(tagNote);
        }
    }
    
    // Update available notification
    if (status.update && status.update.update_available) {
        const updateLink = document.getElementById('updateAvailable');
        updateLink.style.display = 'inline-block';
        const labelFn = window.updateBannerLabel;
        updateLink.textContent = typeof labelFn === 'function'
            ? labelFn(status.update.latest_version, status.self_update_enabled)
            : `Update to ${status.update.latest_version}`;
        updateLink.onclick = (e) => {
            e.preventDefault();
            const canApply = window.canApplyUpdateFromUI?.(status.self_update_enabled) ?? false;
            if (canApply) {
                showUpdateDialog(status.update);
            } else {
                showManualUpdateDialog(status.update);
            }
        };
    } else {
        document.getElementById('updateAvailable').style.display = 'none';
    }
    
    // Update basic stats
    document.getElementById('statCameras').textContent = status.cameras || 0;
    
    // Update timezone selector from server unless the user has unsaved edits
    if (status.timezone && !timezoneDirty) {
        document.getElementById('timezone').value = status.timezone;
    }
    
    // Update queue count
    document.getElementById('statQueue').textContent = status.queued_images || 0;
    
    // Update NTP status
    if (status.time_health) {
        document.getElementById('statNTP').textContent = status.time_health.healthy ? 'OK' : 'WARN';
    } else {
        document.getElementById('statNTP').textContent = '--';
    }
    
    // Update uploads today (if available)
    document.getElementById('statUploads').textContent = status.uploads_today || 0;
    
    // Update system resources display
    updateSystemResourcesDisplay(status.system, status.queued_images);

    updateAPILinkStatusPanel(status.api_link);
    updateStationsDisplay();
}

function updateStationsDisplay() {
    const listEl = document.getElementById('stationList');
    const logEl = document.getElementById('stationPayloadLog');
    if (!listEl || !logEl) {
        return;
    }

    const runtimeById = {};
    (status?.weather?.stations || []).forEach((st) => {
        runtimeById[st.id] = st;
    });
    const ageFn = window.formatObservationAge || ((iso) => iso || 'never');
    const rawFn = window.formatRawPayload || ((raw) => (raw == null ? '' : JSON.stringify(raw, null, 2)));

    if (!stations || stations.length === 0) {
        listEl.innerHTML = `
            <div class="empty-state">
                <p>No weather stations configured yet</p>
                <button class="btn btn-primary" onclick="showAddStation()">Add Station</button>
            </div>`;
    } else {
        listEl.innerHTML = stations.map((cfg) => {
            const rt = runtimeById[cfg.id] || {};
            const lanOk = Boolean(rt.lan_ok);
            let lanLabel = 'LAN down';
            let lanClass = 'error';
            if (cfg.txid == null || cfg.txid === '') {
                lanLabel = 'Waiting for txid';
                lanClass = 'degraded';
            } else if (rt.degraded) {
                lanLabel = 'Degraded';
                lanClass = 'degraded';
            } else if (lanOk) {
                lanLabel = 'LAN OK';
                lanClass = 'active';
            } else if (!rt.last_poll_at) {
                lanLabel = 'Starting';
                lanClass = 'degraded';
            }
            const observed = rt.last_observed_at || '';
            const age = ageFn(observed);
            const err = rt.last_poll_error ? `<div class="station-error">${escapeHtml(rt.last_poll_error)}</div>` : '';
            const host = cfg.host ? escapeHtml(cfg.host) : '—';
            const txidLabel = cfg.txid != null ? `txid ${escapeHtml(String(cfg.txid))}` : 'txid unset';
            return `
            <div class="station-card" data-station-id="${escapeHtml(cfg.id)}">
                <div class="station-card-header">
                    <div>
                        <h3>${escapeHtml(cfg.name || cfg.id)}</h3>
                        <div class="station-card-meta" style="margin-top:0.35rem">
                            <span>${escapeHtml(cfg.type || '')}</span>
                            <span>${host}</span>
                            <span>${txidLabel}</span>
                            <span>Sample age: ${escapeHtml(age)}</span>
                            ${cfg.enabled === false ? '<span class="status-badge">Paused</span>' : ''}
                        </div>
                    </div>
                    <div class="station-card-actions">
                        <span class="status-badge ${lanClass}">${lanLabel}</span>
                        <button class="btn btn-sm" onclick="editStation('${escapeHtml(cfg.id)}')">Edit</button>
                        <button class="btn btn-sm btn-danger" onclick="deleteStation('${escapeHtml(cfg.id)}')">Delete</button>
                    </div>
                </div>
                ${err}
            </div>`;
        }).join('');
    }

    const payloads = status?.weather?.recent_payloads || [];
    if (payloads.length === 0) {
        logEl.innerHTML = `<div class="empty-state"><p>No payloads yet</p></div>`;
        return;
    }

    logEl.innerHTML = payloads.map((entry) => {
        const lan = entry.lan_ok ? 'LAN OK' : 'LAN fail';
        const lanClass = entry.lan_ok ? 'active' : 'error';
        const age = ageFn(entry.observed_at);
        const posted = entry.posted === true ? ' · posted' : (entry.posted === false ? ' · post failed' : '');
        const msg = entry.message ? `<div class="station-error">${escapeHtml(entry.message)}</div>` : '';
        const raw = entry.raw != null
            ? `<pre class="station-raw-payload">${escapeHtml(rawFn(entry.raw))}</pre>`
            : `<div class="form-help">No raw payload</div>`;
        return `
        <div class="station-payload-entry">
            <div class="station-payload-meta">
                <span class="status-badge ${lanClass}">${lan}</span>
                <span class="station-payload-id">${escapeHtml(entry.station_id || '')}</span>
                <span>age ${escapeHtml(age)}</span>
                <span>${escapeHtml(posted)}</span>
            </div>
            ${msg}
            ${raw}
        </div>`;
    }).join('');
}

function showAddStation() {
    showModal('Add Weather Station', getStationFormHtml());
}

function editStation(id) {
    const st = stations.find((s) => s.id === id);
    if (!st) return;
    showModal('Edit Weather Station', getStationFormHtml(st));
}

function getStationFormHtml(st = null) {
    const isEdit = st !== null;
    const poll = st?.poll_interval_seconds || 10;
    const txid = st?.txid != null ? String(st.txid) : '';
    return `
        <form id="stationForm" onsubmit="saveStation(event, ${isEdit ? `'${st.id}'` : 'null'})">
            <div class="form-section">
                <div class="form-section-title">Station</div>
                <div class="form-group">
                    <label for="stName">Display Name</label>
                    <input type="text" id="stName" class="form-control" required
                           value="${escapeHtml(st?.name || '')}"
                           placeholder="e.g., Scappoose Davis">
                    <p class="form-help">
                        ${isEdit
        ? `Internal id <code>${escapeHtml(st.id)}</code> is fixed (core enable binds to this id).`
        : 'A unique station id is generated from this name.'}
                    </p>
                </div>
                <div class="form-group">
                    <label for="stType">Type</label>
                    <select id="stType" class="form-control" required>
                        <option value="davis_weatherlink_live" selected>Davis WeatherLink Live (LAN)</option>
                    </select>
                </div>
                <div class="form-group">
                    <label for="stHost">Host (IP or hostname)</label>
                    <input type="text" id="stHost" class="form-control" required
                           value="${escapeHtml(st?.host || '')}"
                           placeholder="192.168.1.50 or [2001:db8::1]">
                    <p class="form-help">We recommend using a static IP or hostname whenever possible. Enter manually (IPv4, IPv6 with brackets optional, or hostname), or use Discover below after scanning your LAN. Discover scans IPv4 only. Wind direction assumes a true-north vane install.</p>
                </div>
                <div class="form-group">
                    <label for="stDiscoverSubnet">Discover network (CIDR)</label>
                    <div class="form-row" style="align-items:flex-end;gap:var(--space-sm)">
                        <div style="flex:1">
                            <input type="text" id="stDiscoverSubnet" class="form-control"
                                   placeholder="192.168.1.0/24"
                                   value="${escapeHtml(loadDiscoverSubnet())}">
                        </div>
                        <button type="button" class="btn" onclick="discoverStations()">Discover</button>
                    </div>
                    <p class="form-help">Required for HTTP scan (IPv4 /24–/30). The bridge runs in Docker and cannot see your LAN prefix automatically. mDNS is best-effort.</p>
                    <div id="stationDiscoverResult" class="camera-test-result" style="margin-top:var(--space-sm)"></div>
                </div>
                <div class="form-group">
                    <label for="stPoll">Poll interval (seconds)</label>
                    <input type="number" id="stPoll" class="form-control" min="10" max="3600" required value="${poll}">
                    <p class="form-help">Davis Local API floor is 10 seconds.</p>
                </div>
                <div class="form-group">
                    <label>
                        <input type="checkbox" id="stEnabled" ${st?.enabled !== false ? 'checked' : ''}>
                        Enabled
                    </label>
                </div>
            </div>
            <div class="form-section">
                <div class="form-section-title">Transmitter (ISS)</div>
                <p class="form-help">Run Test poll, then pick the transmitter id. Required for continuous LAN poll and weather POST.</p>
                <div class="form-group">
                    <label for="stTxid">Transmitter id (txid)</label>
                    <select id="stTxid" class="form-control">
                        <option value="">— Select after Test poll —</option>
                        ${txid !== '' ? `<option value="${escapeHtml(txid)}" selected>txid ${escapeHtml(txid)}</option>` : ''}
                    </select>
                </div>
                <div id="stationTestResult" class="camera-test-result"></div>
            </div>
            <div class="modal-actions">
                <button type="button" class="btn" onclick="testStationPoll()">Test poll</button>
                <button type="button" class="btn" onclick="closeModal()">Cancel</button>
                <button type="submit" class="btn btn-primary">Save</button>
            </div>
        </form>
    `;
}

function buildStationConfigFromForm() {
    const name = document.getElementById('stName')?.value?.trim();
    const host = document.getElementById('stHost')?.value?.trim();
    const type = document.getElementById('stType')?.value || 'davis_weatherlink_live';
    if (!name || !host) return null;
    const poll = parseInt(document.getElementById('stPoll')?.value, 10) || 10;
    const txidRaw = document.getElementById('stTxid')?.value;
    const body = {
        name,
        type,
        host,
        enabled: document.getElementById('stEnabled')?.checked === true,
        poll_interval_seconds: poll,
    };
    if (txidRaw !== '' && txidRaw != null) {
        body.txid = parseInt(txidRaw, 10);
    }
    return body;
}

/** Last Discover CIDR for the station form (session only). */
const DISCOVER_SUBNET_KEY = 'aviationwx_discover_subnet';

function loadDiscoverSubnet() {
    try {
        return sessionStorage.getItem(DISCOVER_SUBNET_KEY) || '';
    } catch {
        return '';
    }
}

function saveDiscoverSubnet(subnet) {
    try {
        const v = String(subnet || '').trim();
        if (v) {
            sessionStorage.setItem(DISCOVER_SUBNET_KEY, v);
        }
    } catch {
        // ignore quota / private mode
    }
}

async function discoverStations() {
    const resultDiv = document.getElementById('stationDiscoverResult');
    const hostInput = document.getElementById('stHost');
    const subnetInput = document.getElementById('stDiscoverSubnet');
    const type = document.getElementById('stType')?.value || 'davis_weatherlink_live';
    const subnet = (subnetInput?.value || '').trim();
    if (!resultDiv) return;

    saveDiscoverSubnet(subnet);

    if (discoverAbort) {
        discoverAbort.abort();
    }
    discoverAbort = new AbortController();
    const signal = discoverAbort.signal;

    resultDiv.innerHTML = `
        <div id="stationDiscoverStatus" class="test-result" style="background: var(--color-bg)">Starting discovery...</div>
        <div id="stationDiscoverProgress" class="discover-progress" hidden>
            <div class="discover-progress-track">
                <div id="stationDiscoverProgressFill" class="discover-progress-fill"></div>
            </div>
            <span id="stationDiscoverProgressLabel" class="discover-progress-label"></span>
        </div>
        <div id="stationDiscoverCandidates"></div>
        <ul id="stationDiscoverNotes" class="form-help" style="margin-top:var(--space-sm)"></ul>`;

    const statusEl = document.getElementById('stationDiscoverStatus');
    const progressEl = document.getElementById('stationDiscoverProgress');
    const progressFill = document.getElementById('stationDiscoverProgressFill');
    const progressLabel = document.getElementById('stationDiscoverProgressLabel');
    const candEl = document.getElementById('stationDiscoverCandidates');
    const notesEl = document.getElementById('stationDiscoverNotes');
    const consume = window.consumeSSEBuffer || ((buf) => ({ events: [], rest: buf }));
    const hostValue = window.discoverHostValue || ((c) => (c?.port && c.port !== 80 ? `${c.host}:${c.port}` : (c?.host || '')));

    let candidateCount = 0;
    let autoFilled = false;
    let probeTotal = 0;

    const updateProgress = (done, total) => {
        if (!progressEl || !progressFill || !progressLabel || !total || total < 1) {
            return;
        }
        probeTotal = total;
        const safeDone = Math.max(0, Math.min(done, total));
        const pct = Math.round((safeDone / total) * 100);
        progressEl.hidden = false;
        progressFill.style.width = `${pct}%`;
        progressLabel.textContent = `${safeDone} / ${total} hosts (${pct}%)`;
    };

    const appendCandidate = (c) => {
        if (!c || !candEl) return;
        candidateCount += 1;
        const hostVal = hostValue(c);
        const ipNote = c.ip && c.ip !== c.host ? ` · ${c.ip}` : '';
        const label =
            (c.name ? c.name + ' · ' : '') + hostVal + ipNote +
            (c.did ? ' · ' + c.did : '') + ' (' + (c.method || '?') + ')';
        const btn = document.createElement('button');
        btn.type = 'button';
        btn.className = 'btn btn-sm';
        btn.style.margin = '0.25rem 0.25rem 0 0';
        btn.textContent = `Use ${label}`;
        btn.addEventListener('click', () => {
            if (hostInput) {
                hostInput.value = hostVal;
            }
            btn.classList.add('btn-primary');
        });
        candEl.appendChild(btn);

        if (!autoFilled && candidateCount === 1 && hostInput && !hostInput.value.trim()) {
            hostInput.value = hostVal;
            autoFilled = true;
        }
    };

    const handleEvent = (ev) => {
        if (!ev || !ev.type) return;
        switch (ev.type) {
            case 'phase':
                if (statusEl) {
                    statusEl.className = 'test-result';
                    statusEl.style.background = 'var(--color-bg)';
                    statusEl.textContent = ev.message || ev.phase || 'Searching...';
                }
                if (ev.phase !== 'http_probe' && progressEl && !probeTotal) {
                    progressEl.hidden = true;
                }
                break;
            case 'progress':
                updateProgress(ev.done || 0, ev.total || 0);
                break;
            case 'note':
                if (notesEl && ev.message) {
                    const li = document.createElement('li');
                    li.textContent = ev.message;
                    notesEl.appendChild(li);
                }
                break;
            case 'candidate':
                appendCandidate(ev.candidate);
                if (statusEl && probeTotal === 0) {
                    statusEl.className = 'test-result success';
                    statusEl.textContent = `Found ${candidateCount} candidate(s) so far...`;
                }
                break;
            case 'done':
                if (probeTotal > 0) {
                    updateProgress(probeTotal, probeTotal);
                }
                if (statusEl) {
                    if (candidateCount === 0) {
                        statusEl.className = 'test-result error';
                        statusEl.textContent = 'No stations found';
                    } else {
                        statusEl.className = 'test-result success';
                        statusEl.textContent = `Found ${candidateCount} candidate(s)`;
                    }
                }
                break;
            case 'error':
                if (statusEl) {
                    statusEl.className = 'test-result error';
                    statusEl.textContent = ev.message || 'Discover failed';
                }
                break;
            default:
                break;
        }
    };

    try {
        const response = await fetchWithAuth('/api/test/station-discover', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                Accept: 'text/event-stream',
            },
            body: JSON.stringify({ type, subnet }),
            signal,
        });
        if (!response.ok) {
            const error = await response.text();
            throw new Error(error || `HTTP ${response.status}`);
        }
        if (!response.body || !response.body.getReader) {
            throw new Error('Streaming response unsupported');
        }

        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let buffer = '';
        while (true) {
            const { done, value } = await reader.read();
            if (done) {
                break;
            }
            buffer += decoder.decode(value, { stream: true });
            const parsed = consume(buffer);
            buffer = parsed.rest;
            for (const ev of parsed.events || []) {
                handleEvent(ev);
            }
        }
        if (buffer.trim()) {
            const parsed = consume(buffer + '\n\n');
            for (const ev of parsed.events || []) {
                handleEvent(ev);
            }
        }
    } catch (err) {
        if (err && err.name === 'AbortError') {
            return;
        }
        if (statusEl) {
            statusEl.className = 'test-result error';
            statusEl.textContent = err.message || String(err);
        } else {
            resultDiv.innerHTML = `<div class="test-result error">${escapeHtml(err.message || String(err))}</div>`;
        }
    }
}

async function testStationPoll() {
    const resultDiv = document.getElementById('stationTestResult');
    if (!resultDiv) return;
    const body = buildStationConfigFromForm();
    if (!body) {
        resultDiv.innerHTML = '<div class="test-result error">✗ Name and host are required</div>';
        return;
    }
    resultDiv.innerHTML = '<div class="test-result" style="background: var(--color-bg)">Polling LAN station...</div>';
    try {
        const resp = await api('/test/station-poll', {
            method: 'POST',
            body: JSON.stringify(body),
        });
        const result = resp.result || resp;
        const transmitters = result.transmitters || [];
        const select = document.getElementById('stTxid');
        const prev = select?.value || '';
        if (select) {
            const iss = transmitters.filter((t) => t.data_structure_type === 1 || t.txid);
            const seen = new Set();
            const options = ['<option value="">— Select transmitter —</option>'];
            iss.forEach((t) => {
                if (seen.has(t.txid)) return;
                seen.add(t.txid);
                const label = `txid ${t.txid}` +
                    (t.temp_f != null ? ` · ${t.temp_f}°F` : '') +
                    (t.rx_state != null ? ` · rx ${t.rx_state}` : '');
                options.push(`<option value="${t.txid}">${escapeHtml(label)}</option>`);
            });
            select.innerHTML = options.join('');
            if (prev && seen.has(Number(prev))) {
                select.value = prev;
            } else if (seen.size === 1) {
                select.value = String([...seen][0]);
            }
        }
        const ageFn = window.formatObservationAge || (() => '');
        const obsNote = result.observed_at
            ? `observed_at ${result.observed_at} (${ageFn(result.observed_at)})`
            : 'no station timestamp (POST would be skipped)';
        resultDiv.innerHTML = `
            <div class="test-result success">✓ Poll OK · ${transmitters.length} transmitter record(s) · ${escapeHtml(obsNote)}</div>
            ${result.provider_meta?.raw
        ? `<pre class="station-raw-payload">${escapeHtml((window.formatRawPayload || ((r) => JSON.stringify(r, null, 2)))(result.provider_meta.raw))}</pre>`
        : ''}`;
    } catch (err) {
        resultDiv.innerHTML = `<div class="test-result error">✗ ${escapeHtml(err.message)}</div>`;
    }
}

async function saveStation(event, existingId) {
    event.preventDefault();
    const body = buildStationConfigFromForm();
    if (!body) {
        alert('Name and host are required');
        return;
    }
    if (body.txid == null) {
        if (!confirm('No transmitter (txid) selected. Save anyway? Continuous poll will wait until you pick one.')) {
            return;
        }
    }
    try {
        if (existingId && existingId !== 'null') {
            await api(`/stations/${existingId}`, {
                method: 'PUT',
                body: JSON.stringify(body),
            });
        } else {
            await api('/stations', {
                method: 'POST',
                body: JSON.stringify(body),
            });
        }
        closeModal();
        await loadStations();
        await refreshStatus();
    } catch (err) {
        alert('Failed to save station: ' + err.message);
    }
}

async function deleteStation(id) {
    if (!confirm('Delete this weather station?')) {
        return;
    }
    try {
        await api(`/stations/${id}`, { method: 'DELETE' });
        await loadStations();
        await refreshStatus();
    } catch (err) {
        alert('Failed to delete station: ' + err.message);
    }
}

// System resources display
function updateSystemResourcesDisplay(system, queueImages) {
    // CPU
    const cpuPercent = system?.cpu_percent || 0;
    const cpuLevel = 'healthy'; // Simple threshold for now
    updateResourceBar('cpu', cpuPercent, cpuLevel, `${cpuPercent.toFixed(0)}%`);
    
    // Memory
    const memPercent = system?.mem_percent || 0;
    const memLevel = 'healthy';
    const memUsed = system?.mem_used_mb || 0;
    updateResourceBar('mem', memPercent, memLevel, `${memPercent.toFixed(0)}%`);
    
    // Queue (use percentage based on some reasonable max)
    const queuePercent = 0; // We'll use count instead
    const queueLevel = 'healthy';
    updateResourceBar('queue', queuePercent, queueLevel, `${queuePercent.toFixed(0)}%`);
    
    // Overall badge
    const badgeEl = document.getElementById('systemOverallBadge');
    if (badgeEl) {
        badgeEl.classList.remove('healthy', 'warning', 'critical');
        badgeEl.classList.add('healthy');
        badgeEl.textContent = 'Healthy';
    }
    
    // Details text
    const detailsEl = document.getElementById('resourceDetailsText');
    if (detailsEl) {
        const uptime = system?.uptime || '--';
        detailsEl.textContent = `CPU: ${cpuPercent.toFixed(0)}% • Memory: ${memUsed.toFixed(0)} MB • Queue: ${queueImages || 0} images`;
    }
}

// Update a single resource bar with level coloring
function updateResourceBar(name, percent, level, valueText) {
    const valueEl = document.getElementById(`${name}Value`);
    const barEl = document.getElementById(`${name}Bar`);
    
    if (valueEl) {
        valueEl.textContent = valueText;
    }
    
    if (barEl) {
        barEl.style.width = `${Math.min(percent, 100)}%`;
        barEl.classList.remove('healthy', 'warning', 'critical');
        barEl.classList.add(level);
    }
}

// Camera displays
function updateCameraOverview() {
    const container = document.getElementById('cameraOverview');
    
    if (cameras.length === 0) {
        container.innerHTML = `
            <div class="empty-state">
                <p>No cameras configured</p>
                <button class="btn btn-primary" onclick="showSection('cameras'); showAddCamera()">Add Camera</button>
            </div>
        `;
        return;
    }
    
    container.innerHTML = cameras.map(cam => {
        // Get camera stats from orchestrator
        let statusBadge = '';
        let nextCaptureInfo = '';
        if (cam.enabled && status && status.orchestrator) {
            const camStats = status.orchestrator.camera_stats?.find(cs => cs.camera_id === cam.id);
            if (camStats) {
                if (camStats.capture_stats?.currently_capturing) {
                    statusBadge = '<span class="status-badge capturing">Capturing</span>';
                } else if (camStats.capture_stats?.next_capture_time) {
                    const nextTime = new Date(camStats.capture_stats.next_capture_time);
                    const now = new Date();
                    const secondsUntil = Math.max(0, Math.floor((nextTime - now) / 1000));
                    nextCaptureInfo = `<span class="next-capture">Next: ${secondsUntil}s</span>`;
                }
                
                // Check for upload issues
                const uploadStats = status.orchestrator.upload_stats;
                if (uploadStats && uploadStats.per_camera_failures) {
                    const failures = uploadStats.per_camera_failures[cam.id] || 0;
                    if (failures > 0) {
                        statusBadge += ` <span class="status-badge error">⚠️ ${failures} upload failures</span>`;
                    }
                }
            }
        }
        
        return `
        <div class="camera-overview-item" data-camera-id="${cam.id}">
            <div class="camera-preview">
                <img alt="${escapeHtml(cam.name)}"
                     class="camera-preview-img"
                     data-camera-id="${cam.id}"
                     style="display:none">
                <span style="display:flex">No Preview</span>
            </div>
            <div class="camera-info">
                <div class="camera-name">${escapeHtml(cam.name)}</div>
                <div class="camera-meta">${cam.type} • ${cam.capture_interval_seconds}s interval</div>
                <div class="camera-status-info">${nextCaptureInfo} ${statusBadge}</div>
            </div>
            <div class="camera-status">
                <span class="camera-status-badge ${cam.enabled ? 'active' : 'paused'}">
                    ${cam.enabled ? 'Active' : 'Paused'}
                </span>
            </div>
        </div>
    `}).join('');
    
    hydratePreviewImages();
    startSmoothImageRefresh();
    initLastDisplayedCaptureTimes();
}

function initLastDisplayedCaptureTimes() {
    if (!status?.orchestrator) return;
    status.orchestrator.camera_stats?.forEach((cs) => {
        const t = cs.capture_stats?.last_capture_time;
        if (t) {
            lastDisplayedCaptureTime.set(cs.camera_id, new Date(t).getTime());
        }
    });
}

function updateCameraOverviewStatus() {
    if (!status?.orchestrator) return;
    document.querySelectorAll('.camera-overview-item[data-camera-id]').forEach((item) => {
        const cameraId = item.dataset.cameraId;
        const cam = cameras.find((c) => c.id === cameraId);
        if (!cam) return;

        let statusBadge = '';
        let nextCaptureInfo = '';
        const camStats = status.orchestrator.camera_stats?.find((cs) => cs.camera_id === cameraId);
        if (cam.enabled && camStats) {
            if (camStats.capture_stats?.currently_capturing) {
                statusBadge = '<span class="status-badge capturing">Capturing</span>';
            } else if (camStats.capture_stats?.next_capture_time) {
                const nextTime = new Date(camStats.capture_stats.next_capture_time);
                const now = new Date();
                const secondsUntil = Math.max(0, Math.floor((nextTime - now) / 1000));
                nextCaptureInfo = `<span class="next-capture">Next: ${secondsUntil}s</span>`;
            }
            const uploadStats = status.orchestrator.upload_stats;
            if (uploadStats?.per_camera_failures) {
                const failures = uploadStats.per_camera_failures[cameraId] || 0;
                if (failures > 0) {
                    statusBadge += ` <span class="status-badge error">⚠️ ${failures} upload failures</span>`;
                }
            }
        }

        const statusInfo = item.querySelector('.camera-status-info');
        if (statusInfo) {
            statusInfo.innerHTML = `${nextCaptureInfo} ${statusBadge}`;
        }

        const badge = item.querySelector('.camera-status-badge');
        if (badge) {
            badge.textContent = cam.enabled ? 'Active' : 'Paused';
            badge.className = `camera-status-badge ${cam.enabled ? 'active' : 'paused'}`;
        }
    });
}

function updateCameraList() {
    const container = document.getElementById('cameraList');
    
    if (cameras.length === 0) {
        container.innerHTML = `
            <div class="empty-state">
                <p>No cameras configured yet</p>
            </div>
        `;
        return;
    }
    
    container.innerHTML = cameras.map((cam) => {
        const detailsHtml = buildCameraCardDetails(cam);
        return `
        <div class="camera-card" data-camera-id="${cam.id}">
            <div class="camera-card-header">
                <div class="camera-card-title">
                    <span class="camera-status-badge ${cam.enabled ? 'active' : 'paused'}">
                        ${cam.enabled ? 'Active' : 'Paused'}
                    </span>
                    <h3>${escapeHtml(cam.name)}</h3>
                </div>
                <div class="camera-card-actions">
                    <button class="btn btn-sm" onclick="editCamera('${cam.id}')">Edit</button>
                    <button class="btn btn-sm btn-danger" onclick="deleteCamera('${cam.id}')">Delete</button>
                </div>
            </div>
            <div class="camera-card-body">
                <div class="camera-card-preview">
                    <img alt="${escapeHtml(cam.name)}"
                         class="camera-preview-img"
                         data-camera-id="${cam.id}"
                         style="display:none">
                    <span style="display:flex">Preview not available</span>
                </div>
                <div class="camera-card-details" data-camera-id="${cam.id}">${detailsHtml}</div>
            </div>
        </div>
    `}).join('');
    hydratePreviewImages();
    startSmoothImageRefresh();
}

function buildCameraCardDetails(cam) {
    let captureStatusText = '';
    if (cam.worker_running && status && status.orchestrator) {
        const camStats = status.orchestrator.camera_stats?.find((cs) => cs.camera_id === cam.id);
        if (camStats) {
            if (camStats.capture_stats?.currently_capturing) {
                captureStatusText = '<span class="status-active">🔴 Capturing now</span>';
            } else if (camStats.capture_stats?.next_capture_time) {
                const nextTime = new Date(camStats.capture_stats.next_capture_time);
                const now = new Date();
                const secondsUntil = Math.max(0, Math.floor((nextTime - now) / 1000));
                captureStatusText = `<span class="status-info">Next: ${secondsUntil}s</span>`;
            }
        }
    }
    let uploadStatusText = '';
    if (status?.orchestrator?.upload_stats?.currently_uploading) {
        uploadStatusText = '<span class="status-active">🔴 Uploading</span>';
    }
    let uploadFailureText = '';
    if (status?.orchestrator?.upload_stats) {
        const failures = status.orchestrator.upload_stats.per_camera_failures?.[cam.id] || 0;
        const lastReason = status.orchestrator.upload_stats.last_failure_reason;
        if (failures > 0) {
            uploadFailureText = `
                <div class="detail-row error">
                    <span class="label">Upload Failures</span>
                    <span class="value">${failures}</span>
                </div>
            `;
            if (lastReason) {
                uploadFailureText += `
                    <div class="detail-row error">
                        <span class="label">Last Error</span>
                        <span class="value">${escapeHtml(lastReason.substring(0, 50))}</span>
                    </div>
                `;
            }
        }
    }
    let captureErrorText = '';
    if (status?.orchestrator) {
        const camStats = status.orchestrator.camera_stats?.find((cs) => cs.camera_id === cam.id);
        if (camStats?.last_error?.Message) {
            const errorMsg = camStats.last_error.Message;
            let shortError = errorMsg;
            if (errorMsg.includes('Connection refused')) {
                shortError = 'Connection refused - RTSP server not available';
            } else if (errorMsg.includes('timed out')) {
                shortError = 'Connection timed out';
            } else if (errorMsg.includes('Authentication')) {
                shortError = 'Authentication failed';
            } else {
                const lines = errorMsg.split('\n').filter((l) => l.trim());
                shortError = lines[lines.length - 1] || errorMsg.substring(0, 80);
            }
            captureErrorText = `
                <div class="detail-row error">
                    <span class="label">⚠️ Capture Error</span>
                    <span class="value">${escapeHtml(shortError)}</span>
                </div>
            `;
            if (camStats.is_backing_off) {
                captureErrorText += `
                    <div class="detail-row error">
                        <span class="label">Status</span>
                        <span class="value">Backing off, will retry soon</span>
                    </div>
                `;
            }
        }
    }
    let queueCount = 0;
    let queueStatusClass = 'healthy';
    if (status?.orchestrator) {
        const camStats = status.orchestrator.camera_stats?.find((cs) => cs.camera_id === cam.id);
        if (camStats?.queue_stats) {
            queueCount = camStats.queue_stats.image_count || 0;
            const queuePercent = (queueCount / 50) * 100;
            if (queuePercent > 80) queueStatusClass = 'critical';
            else if (queuePercent > 50) queueStatusClass = 'warning';
        }
    }
    return `
        <div class="detail-row">
            <span class="label">Type</span>
            <span class="value">${cam.type}</span>
        </div>
        <div class="detail-row">
            <span class="label">Capture Interval</span>
            <span class="value">${cam.capture_interval_seconds}s</span>
        </div>
        <div class="detail-row">
            <span class="label">Status</span>
            <span class="value">${captureStatusText}${uploadStatusText ? ' | ' + uploadStatusText : ''}</span>
        </div>
        <div class="detail-row">
            <span class="label">Upload User</span>
            <span class="value">${cam.upload?.username || 'Not configured'}</span>
        </div>
        <div class="detail-row">
            <span class="label">Upload Host</span>
            <span class="value">${cam.upload?.host || 'upload.aviationwx.org'}</span>
        </div>
        ${captureErrorText}
        ${uploadFailureText}
        <div class="queue-health">
            <div class="queue-health-label">
                <span>Queue</span>
                <span>${queueCount} files</span>
            </div>
            <div class="queue-health-bar">
                <div class="queue-health-fill ${queueStatusClass}" style="width: ${Math.min(100, (queueCount / 50) * 100)}%"></div>
            </div>
        </div>
    `;
}

function updateCameraListStatus() {
    if (!status?.orchestrator) return;
    document.querySelectorAll('.camera-card[data-camera-id]').forEach((card) => {
        const cameraId = card.dataset.cameraId;
        const cam = cameras.find((c) => c.id === cameraId);
        if (!cam) return;
        const detailsEl = card.querySelector('.camera-card-details');
        if (detailsEl) {
            detailsEl.innerHTML = buildCameraCardDetails(cam);
        }
    });
}

// Time management
function populateTimezones() {
    const select = document.getElementById('timezone');
    TIMEZONES.forEach(tz => {
        const option = document.createElement('option');
        option.value = tz.value;
        option.textContent = tz.label;
        select.appendChild(option);
    });
}

function startTimeUpdates() {
    updateTimeDisplay();
    timeUpdateInterval = setInterval(updateTimeDisplay, 1000);
}

let refreshDelayMs = 5000;
let refreshFailures = 0;

function startAutoRefresh() {
    const tick = async () => {
        try {
            if (!(await refreshStatus())) {
                throw new Error('status refresh failed');
            }

            const activeSection = document.querySelector('.section.active');
            if (activeSection && (activeSection.id === 'dashboard' || activeSection.id === 'cameras')) {
                await loadCameras();
            }
            refreshFailures = 0;
            refreshDelayMs = 5000;
        } catch (error) {
            console.error('Auto-refresh error:', error);
            refreshFailures++;
            refreshDelayMs = Math.min(30000, 5000 * (2 ** Math.min(refreshFailures, 3)));
        }
        setTimeout(tick, refreshDelayMs);
    };
    tick();
}

function updateTimeDisplay() {
    const now = new Date();
    const utc = now.toISOString();
    
    // UTC display
    document.getElementById('utcTime').textContent = formatTime(now, 'UTC');
    
    // Local display based on configured timezone
    const tz = document.getElementById('timezone').value || Intl.DateTimeFormat().resolvedOptions().timeZone;
    document.getElementById('localTime').textContent = formatTime(now, tz);
    
    // Header time display
    document.getElementById('timeDisplay').textContent = `UTC ${formatTime(now, 'UTC')}`;
}

function formatTime(date, timezone) {
    try {
        return date.toLocaleTimeString('en-US', {
            timeZone: timezone,
            hour: '2-digit',
            minute: '2-digit',
            second: '2-digit',
            hour12: false
        });
    } catch {
        return '--:--:--';
    }
}

function updateSettingsUnsavedHints() {
    const set = (id, dirty) => {
        const el = document.getElementById(id);
        if (el) el.style.display = dirty ? 'block' : 'none';
    };
    set('timezoneUnsavedHint', timezoneDirty);
    set('webConsoleUnsavedHint', webConsoleDirty);
    set('uploadSettingsUnsavedHint', uploadSettingsDirty);
    set('apiLinkUnsavedHint', apiLinkDirty);
}

function markTimezoneDirty() {
    timezoneDirty = true;
    updateSettingsUnsavedHints();
}

function markWebConsoleDirty() {
    webConsoleDirty = true;
    updateSettingsUnsavedHints();
}

function markUploadSettingsDirty() {
    uploadSettingsDirty = true;
    updateSettingsUnsavedHints();
}

function markAPILinkDirty() {
    apiLinkDirty = true;
    updateSettingsUnsavedHints();
}

async function saveTimezone() {
    const timezone = document.getElementById('timezone').value;
    try {
        await api('/time', {
            method: 'PUT',
            body: JSON.stringify({ timezone }),
        });
    } catch (err) {
        alert('Failed to save timezone: ' + err.message);
        return;
    }
    timezoneDirty = false;
    updateSettingsUnsavedHints();
    await loadConfig();
    updateTimeDisplay();
    showNotification('✅ Timezone updated! Workers reloaded automatically.', 'success');
}

// Notification system
function showNotification(message, type = 'info') {
    const notification = document.createElement('div');
    notification.className = `notification notification-${type}`;
    notification.textContent = message;
    notification.style.cssText = `
        position: fixed;
        top: 80px;
        right: 20px;
        padding: 15px 20px;
        background: ${type === 'success' ? '#28a745' : type === 'warning' ? '#ffc107' : '#007bff'};
        color: white;
        border-radius: 4px;
        box-shadow: 0 2px 8px rgba(0,0,0,0.2);
        z-index: 10000;
        animation: slideIn 0.3s ease-out;
    `;
    document.body.appendChild(notification);
    
    setTimeout(() => {
        notification.style.animation = 'slideOut 0.3s ease-in';
        setTimeout(() => notification.remove(), 300);
    }, 3000);
}

// Camera management
function showAddCamera() {
    showModal('Add Camera', getCameraFormHtml());
}

function editCamera(id) {
    const cam = cameras.find(c => c.id === id);
    if (!cam) return;
    showModal('Edit Camera', getCameraFormHtml(cam));
}

function getCameraFormHtml(cam = null) {
    const isEdit = cam !== null;
    return `
        <form id="cameraForm" onsubmit="saveCamera(event, ${isEdit ? `'${cam.id}'` : 'null'})">
            <div class="form-section">
                <div class="form-section-title">Camera Details</div>
                
                <div class="form-group">
                    <label for="camName">Display Name</label>
                    <input type="text" id="camName" class="form-control" required
                           value="${cam?.name || ''}"
                           placeholder="e.g., KORD West Camera">
                    <p class="form-help">
                        ${isEdit
        ? `Internal id <code>${escapeHtml(cam.id)}</code> is fixed (queue and paths).`
        : 'A unique camera id is generated from this name (e.g. &quot;KORD West&quot; → <code>kord-west</code>). If the name matches an existing camera, a suffix is added automatically.'}
                    </p>
                </div>
                <input type="hidden" id="camExistingId" value="${isEdit ? escapeHtml(cam.id) : ''}">
                
                <div class="form-group">
                    <label for="camType">Camera Type</label>
                    <select id="camType" class="form-control" required onchange="updateCameraTypeFields()">
                        <option value="">-- Select Type --</option>
                        <option value="http" ${cam?.type === 'http' ? 'selected' : ''}>HTTP Snapshot</option>
                        <option value="rtsp" ${cam?.type === 'rtsp' ? 'selected' : ''}>RTSP Stream</option>
                        <option value="onvif" ${cam?.type === 'onvif' ? 'selected' : ''}>ONVIF Camera</option>
                    </select>
                </div>
                
                <div id="httpFields" style="display: ${cam?.type === 'http' ? 'block' : 'none'}">
                    <div class="form-group">
                        <label for="camSnapshotUrl">Snapshot URL</label>
                        <input type="url" id="camSnapshotUrl" class="form-control" 
                               value="${cam?.snapshot_url || ''}"
                               placeholder="http://192.168.1.100/snapshot.jpg">
                    </div>
                    <div class="form-row">
                        <div class="form-group">
                            <label for="camAuthUser">Camera Username</label>
                            <input type="text" id="camAuthUser" class="form-control" 
                                   value="${cam?.auth?.username || ''}"
                                   placeholder="admin">
                        </div>
                        <div class="form-group">
                            <label for="camAuthPass">Camera Password</label>
                            <input type="password" id="camAuthPass" class="form-control" 
                                   placeholder="••••••••">
                        </div>
                    </div>
                </div>
                
                <div id="rtspFields" style="display: ${cam?.type === 'rtsp' ? 'block' : 'none'}">
                    <div class="form-group">
                        <label for="camRtspUrl">RTSP URL</label>
                        <input type="text" id="camRtspUrl" class="form-control" 
                               value="${cam?.rtsp?.url || ''}"
                               placeholder="rtsp://192.168.1.100:554/stream">
                    </div>
                    <div class="form-row">
                        <div class="form-group">
                            <label for="camRtspUser">RTSP Username</label>
                            <input type="text" id="camRtspUser" class="form-control" 
                                   value="${cam?.rtsp?.username || ''}"
                                   placeholder="admin">
                        </div>
                        <div class="form-group">
                            <label for="camRtspPass">RTSP Password</label>
                            <input type="password" id="camRtspPass" class="form-control" 
                                   placeholder="••••••••">
                        </div>
                    </div>
                </div>
                
                <div id="onvifFields" style="display: ${cam?.type === 'onvif' ? 'block' : 'none'}">
                    <div class="form-group">
                        <label for="camOnvifEndpoint">ONVIF Endpoint</label>
                        <input type="text" id="camOnvifEndpoint" class="form-control" 
                               value="${cam?.onvif?.endpoint || ''}"
                               placeholder="http://192.168.1.100/onvif/device_service">
                        <small>Full ONVIF device service URL</small>
                    </div>
                    <div class="form-row">
                        <div class="form-group">
                            <label for="camOnvifUser">ONVIF Username</label>
                            <input type="text" id="camOnvifUser" class="form-control" 
                                   value="${cam?.onvif?.username || ''}"
                                   placeholder="admin">
                        </div>
                        <div class="form-group">
                            <label for="camOnvifPass">ONVIF Password</label>
                            <input type="password" id="camOnvifPass" class="form-control" 
                                   placeholder="••••••••">
                        </div>
                    </div>
                    <div class="form-group">
                        <label for="camOnvifProfile">Profile Token (optional)</label>
                        <input type="text" id="camOnvifProfile" class="form-control" 
                               value="${cam?.onvif?.profile_token || ''}"
                               placeholder="profile_1">
                        <small>Leave empty to use default profile</small>
                    </div>
                </div>
                
                <div class="form-group">
                    <label for="camInterval">Capture Interval (seconds)</label>
                    <input type="number" id="camInterval" class="form-control" 
                           value="${cam?.capture_interval_seconds || 60}"
                           min="1" max="1800" required>
                    <p class="form-help">How often to capture images (1 second to 30 minutes)</p>
                </div>
                
                <button type="button" class="btn" onclick="testCamera()">Test Snapshot</button>
                <div id="cameraTestResult" class="camera-test-result"></div>
            </div>
            
            <div class="form-section">
                <div class="form-section-title">Image Quality & Bandwidth</div>
                <p class="form-help" style="margin-bottom: var(--space-md)">
                    Control image resolution and quality to manage bandwidth usage.
                </p>
                
                <div class="form-group">
                    <label for="imagePreset">Quality Preset</label>
                    <select id="imagePreset" class="form-control" onchange="updateImagePreset()">
                        <option value="original" ${!cam?.image?.max_width ? 'selected' : ''}>Original (no processing)</option>
                        <option value="high" ${cam?.image?.max_width === 1920 ? 'selected' : ''}>High (1080p, quality 90)</option>
                        <option value="medium" ${cam?.image?.max_width === 1280 ? 'selected' : ''}>Medium (720p, quality 80)</option>
                        <option value="low" ${cam?.image?.max_width === 640 ? 'selected' : ''}>Low (480p, quality 70)</option>
                        <option value="custom">Custom</option>
                    </select>
                </div>
                
                <div id="customImageSettings" style="display: none;">
                    <div class="form-row">
                        <div class="form-group">
                            <label for="imageMaxWidth">Max Width (px)</label>
                            <input type="number" id="imageMaxWidth" class="form-control" 
                                   value="${cam?.image?.max_width || ''}"
                                   min="0" max="4096" placeholder="0 = original">
                        </div>
                        <div class="form-group">
                            <label for="imageMaxHeight">Max Height (px)</label>
                            <input type="number" id="imageMaxHeight" class="form-control" 
                                   value="${cam?.image?.max_height || ''}"
                                   min="0" max="4096" placeholder="0 = original">
                        </div>
                    </div>
                    <div class="form-group">
                        <label for="imageQuality">JPEG Quality (1-100)</label>
                        <input type="range" id="imageQuality" class="form-control" 
                               value="${cam?.image?.quality || 85}"
                               min="1" max="100" oninput="document.getElementById('qualityValue').textContent = this.value">
                        <div style="display: flex; justify-content: space-between; font-size: 0.8rem; color: var(--color-text-muted);">
                            <span>Smaller files</span>
                            <span id="qualityValue">${cam?.image?.quality || 85}</span>
                            <span>Better quality</span>
                        </div>
                    </div>
                </div>
            </div>
            
            <div class="form-section">
                <div class="form-section-title">SFTP upload (this camera)</div>
                <p class="form-help" style="margin-bottom: var(--space-md)">
                    Request a dedicated SFTP account for this camera from <a href="mailto:contact@aviationwx.org">contact@aviationwx.org</a>.
                    Do not reuse the same host, username, and port for another camera — each camera must have its own credentials.
                </p>
                
                <div class="form-row">
                    <div class="form-group">
                        <label for="uploadUser">SFTP username (this camera)</label>
                        <input type="text" id="uploadUser" class="form-control" 
                               value="${cam?.upload?.username || ''}"
                               required placeholder="e.g. account assigned to this camera">
                    </div>
                    <div class="form-group">
                        <label for="uploadPass">SFTP password (this camera)</label>
                        <div class="password-input-row">
                            <input type="password" id="uploadPass" class="form-control" spellcheck="false"
                                   ${isEdit ? 'autocomplete="current-password" placeholder="Leave blank to keep current password"' : 'autocomplete="new-password" required placeholder="Password for this camera account"'}
                            >
                            <button type="button" class="btn btn-secondary btn-sm" id="uploadPassToggle" aria-pressed="false" aria-label="Show SFTP password" onclick="togglePasswordField('uploadPass', 'uploadPassToggle', 'SFTP password')">Show</button>
                        </div>
                    </div>
                </div>
                
                <div class="form-group">
                    <label for="uploadHost">Upload Server</label>
                    <input type="text" id="uploadHost" class="form-control" 
                           value="${cam?.upload?.host || 'upload.aviationwx.org'}"
                           readonly>
                    <p class="form-help">Default server for aviationwx.org</p>
                </div>
                
                <div class="form-group">
                    <label for="uploadPort">Upload Server Port</label>
                    <input type="number" id="uploadPort" class="form-control" 
                           value="${cam?.upload?.port || 2222}"
                           min="1" max="65535" required>
                    <p class="form-help">SFTP port (default: 2222)</p>
                </div>
                
                <div class="form-group">
                    <label for="uploadBasePath">Upload Directory</label>
                    <input type="text" id="uploadBasePath" class="form-control" 
                           value="${cam?.upload?.base_path || '/files'}"
                           placeholder="/files">
                    <p class="form-help">SFTP base directory for uploads (default: /files)</p>
                </div>
                
                <button type="button" class="btn" onclick="testUpload()">Test Connection</button>
                <div id="uploadTestResult"></div>
            </div>
            
            <div class="form-group">
                <label>
                    <input type="checkbox" id="camEnabled" ${cam?.enabled !== false ? 'checked' : ''}>
                    Enable camera
                </label>
            </div>
            
            <div style="display: flex; gap: var(--space-md); justify-content: flex-end; margin-top: var(--space-lg);">
                <button type="button" class="btn" onclick="closeModal()">Cancel</button>
                <button type="submit" class="btn btn-primary">${isEdit ? 'Save Changes' : 'Add Camera'}</button>
            </div>
        </form>
    `;
}

function updateCameraTypeFields() {
    const type = document.getElementById('camType').value;
    document.getElementById('httpFields').style.display = type === 'http' ? 'block' : 'none';
    document.getElementById('rtspFields').style.display = type === 'rtsp' ? 'block' : 'none';
    document.getElementById('onvifFields').style.display = type === 'onvif' ? 'block' : 'none';
}

function updateImagePreset() {
    const preset = document.getElementById('imagePreset').value;
    const customDiv = document.getElementById('customImageSettings');
    
    if (preset === 'custom') {
        customDiv.style.display = 'block';
        return;
    }
    
    customDiv.style.display = 'none';
    
    // Apply preset values (these get read by saveCamera)
    const presets = {
        original: { maxWidth: 0, maxHeight: 0, quality: 0 },
        high: { maxWidth: 1920, maxHeight: 1080, quality: 90 },
        medium: { maxWidth: 1280, maxHeight: 720, quality: 80 },
        low: { maxWidth: 640, maxHeight: 480, quality: 70 },
    };
    
    const p = presets[preset];
    if (p) {
        document.getElementById('imageMaxWidth').value = p.maxWidth || '';
        document.getElementById('imageMaxHeight').value = p.maxHeight || '';
        document.getElementById('imageQuality').value = p.quality || 85;
        document.getElementById('qualityValue').textContent = p.quality || 85;
    }
}

async function saveCamera(event, existingId = null) {
    event.preventDefault();

    const uploadHost = document.getElementById('uploadHost').value || 'upload.aviationwx.org';
    const uploadPort = parseInt(document.getElementById('uploadPort').value, 10) || 2222;
    const uploadUser = document.getElementById('uploadUser').value;
    if (typeof window.findConflictingCameraId === 'function') {
        const conflictId = window.findConflictingCameraId(cameras, existingId, uploadHost, uploadPort, uploadUser);
        if (conflictId) {
            alert(`This SFTP account (host, port, and username) is already used by camera "${conflictId}". Each camera must have its own SFTP credentials from aviationwx.org.`);
            return;
        }
    }
    
    const displayName = document.getElementById('camName').value.trim();
    if (!displayName) {
        alert('Please enter a display name.');
        return;
    }

    const type = document.getElementById('camType').value;
    const basePath = document.getElementById('uploadBasePath')?.value || '/files';
    const camera = {
        name: displayName,
        type: type,
        enabled: document.getElementById('camEnabled').checked,
        capture_interval_seconds: parseInt(document.getElementById('camInterval').value, 10),
        upload: {
            protocol: 'sftp',
            host: uploadHost,
            port: uploadPort,
            username: document.getElementById('uploadUser').value,
            password: document.getElementById('uploadPass').value || undefined,
            base_path: basePath,
        }
    };
    if (existingId) {
        camera.id = existingId;
    }
    
    // Image processing settings
    const maxWidth = parseInt(document.getElementById('imageMaxWidth').value, 10) || 0;
    const maxHeight = parseInt(document.getElementById('imageMaxHeight').value, 10) || 0;
    const quality = parseInt(document.getElementById('imageQuality').value, 10) || 0;
    
    if (maxWidth > 0 || maxHeight > 0 || (quality > 0 && quality !== 85)) {
        camera.image = {
            max_width: maxWidth,
            max_height: maxHeight,
            quality: quality,
        };
    }
    
    if (type === 'http') {
        camera.snapshot_url = document.getElementById('camSnapshotUrl').value;
        const authUser = document.getElementById('camAuthUser').value;
        const authPass = document.getElementById('camAuthPass').value;
        if (authUser) {
            camera.auth = {
                type: 'basic',
                username: authUser,
                password: authPass,
            };
        }
    } else if (type === 'rtsp') {
        camera.rtsp = {
            url: document.getElementById('camRtspUrl').value,
            username: document.getElementById('camRtspUser').value,
            password: document.getElementById('camRtspPass').value,
        };
    } else if (type === 'onvif') {
        camera.onvif = {
            endpoint: document.getElementById('camOnvifEndpoint').value,
            username: document.getElementById('camOnvifUser').value,
            password: document.getElementById('camOnvifPass').value,
            profile_token: document.getElementById('camOnvifProfile').value || undefined,
        };
    }
    
    try {
        if (existingId) {
            await api(`/cameras/${existingId}`, {
                method: 'PUT',
                body: JSON.stringify(camera),
            });
        } else {
            await api('/cameras', {
                method: 'POST',
                body: JSON.stringify(camera),
            });
        }

        cameraFormDirty = false;
        closeModal();
        await loadCameras();
    } catch (err) {
        alert('Failed to save camera: ' + err.message);
    }
}

async function deleteCamera(id) {
    if (!confirm(`Are you sure you want to delete this camera?`)) {
        return;
    }
    
    try {
        await api(`/cameras/${id}`, { method: 'DELETE' });
        await loadCameras();
    } catch (err) {
        alert('Failed to delete camera: ' + err.message);
    }
}

let lastCameraPreviewUrl = null;

async function testCamera() {
    const resultDiv = document.getElementById('cameraTestResult');
    if (lastCameraPreviewUrl) {
        URL.revokeObjectURL(lastCameraPreviewUrl);
        lastCameraPreviewUrl = null;
    }
    resultDiv.innerHTML = '<div class="test-result" style="background: var(--color-bg)">Testing snapshot...</div>';
    resultDiv.className = 'camera-test-result';

    const camera = buildCameraConfigFromForm();
    if (!camera) {
        resultDiv.innerHTML = '<div class="test-result error">✗ Please fill in camera type and required fields</div>';
        return;
    }

    try {
        const response = await fetchWithAuth('/api/test/camera', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(camera),
        });

        if (response.ok) {
            const blob = await response.blob();
            const url = URL.createObjectURL(blob);
            lastCameraPreviewUrl = url;
            resultDiv.innerHTML = `
                <div class="test-result success">✓ Snapshot successful!</div>
                <div class="camera-preview">
                    <img src="${url}" alt="Camera preview">
                </div>
            `;
        } else {
            const errorText = await response.text();
            const msg = errorText.replace(/^Test failed: /, '').trim();
            resultDiv.innerHTML = `<div class="test-result error">✗ ${msg || 'Snapshot failed'}</div>`;
        }
    } catch (err) {
        resultDiv.innerHTML = `<div class="test-result error">✗ ${err.message}</div>`;
    }
}

function buildCameraConfigFromForm() {
    const type = document.getElementById('camType')?.value;
    if (!type) return null;

    const name = document.getElementById('camName')?.value?.trim();
    const existing = document.getElementById('camExistingId')?.value?.trim();
    let provisionalId = existing;
    if (!provisionalId && typeof window.slugCameraIdFromName === 'function') {
        provisionalId = window.slugCameraIdFromName(name);
    }
    const values = {
        type,
        id: provisionalId || undefined,
        snapshot_url: document.getElementById('camSnapshotUrl')?.value,
        auth_user: document.getElementById('camAuthUser')?.value,
        auth_pass: document.getElementById('camAuthPass')?.value,
        rtsp_url: document.getElementById('camRtspUrl')?.value,
        rtsp_user: document.getElementById('camRtspUser')?.value,
        rtsp_pass: document.getElementById('camRtspPass')?.value,
        onvif_endpoint: document.getElementById('camOnvifEndpoint')?.value,
        onvif_user: document.getElementById('camOnvifUser')?.value,
        onvif_pass: document.getElementById('camOnvifPass')?.value,
        onvif_profile: document.getElementById('camOnvifProfile')?.value,
    };
    return window.buildCameraConfigFromFormValues(values);
}

async function testUpload() {
    const resultDiv = document.getElementById('uploadTestResult');
    resultDiv.innerHTML = '<div class="test-result" style="background: var(--color-bg)">Testing connection...</div>';

    try {
        const result = await api('/test/upload', {
            method: 'POST',
            body: JSON.stringify({
                protocol: 'sftp',
                host: document.getElementById('uploadHost').value || 'upload.aviationwx.org',
                port: parseInt(document.getElementById('uploadPort').value) || 2222,
                username: document.getElementById('uploadUser').value,
                password: document.getElementById('uploadPass').value,
                base_path: document.getElementById('uploadBasePath')?.value || '/files',
            }),
        });

        if (result.status === 'ok') {
            resultDiv.innerHTML = '<div class="test-result success">✓ SFTP connection successful!</div>';
        } else {
            resultDiv.innerHTML = `<div class="test-result error">✗ ${result.error || 'Connection failed'}</div>`;
        }
    } catch (err) {
        resultDiv.innerHTML = `<div class="test-result error">✗ ${err.message}</div>`;
    }
}

// Settings
async function saveWebSettings() {
    const password = document.getElementById('webPassword').value;
    if (!password) {
        alert('Please enter a password');
        return;
    }

    if (!config) {
        await loadConfig();
    }
    if (!config) {
        alert('Unable to load settings. Refresh the page and try again.');
        return;
    }

    try {
        // Handler replaces web_console wholesale; merge with GET snapshot so listen port is not cleared.
        const wc = { ...((config && config.web_console) || {}) };
        if (!wc.port || wc.port <= 0) {
            wc.port = DEFAULT_WEB_CONSOLE_PORT;
        }
        await api('/config', {
            method: 'PUT',
            body: JSON.stringify({
                web_console: {
                    ...wc,
                    enabled: true,
                    password: password,
                },
            }),
        });
        document.getElementById('webPassword').value = '';
        webConsoleDirty = false;
        updateSettingsUnsavedHints();
        showNotification('✅ Password updated successfully.', 'success');
        await loadConfig();
    } catch (err) {
        alert('Failed to save: ' + err.message);
    }
}

// Global Settings (concurrent uploads, update channel, timeouts)
async function loadGlobalSettings() {
    if (!config) return;
    if (timezoneDirty || webConsoleDirty || uploadSettingsDirty) {
        updateSettingsUnsavedHints();
        return;
    }

    // Load max concurrent uploads (from top-level, not nested in global)
    const maxConcurrent = config.max_concurrent_uploads || 2;
    const maxConcurrentSelect = document.getElementById('maxConcurrentUploads');
    if (maxConcurrentSelect) {
        maxConcurrentSelect.value = maxConcurrent.toString();
    }

    const maxCapturesSelect = document.getElementById('maxConcurrentCaptures');
    if (maxCapturesSelect) {
        if (config.max_concurrent_captures_auto === true) {
            maxCapturesSelect.value = '0';
        } else {
            const n = config.max_concurrent_captures;
            const maxCaptures = (typeof n === 'number' && n >= 1 && n <= 10) ? n : 2;
            maxCapturesSelect.value = maxCaptures.toString();
        }
    }
    
    // Load update channel
    const updateChannel = config.update_channel || 'latest';
    const updateChannelSelect = document.getElementById('updateChannel');
    if (updateChannelSelect) {
        updateChannelSelect.value = updateChannel;
    }
    
    // Load timeout settings
    const timeoutConnect = config.timeout_connect_seconds || 60;
    const timeoutConnectInput = document.getElementById('timeoutConnect');
    if (timeoutConnectInput) {
        timeoutConnectInput.value = timeoutConnect;
    }
    
    const timeoutUpload = config.timeout_upload_seconds || 300;
    const timeoutUploadInput = document.getElementById('timeoutUpload');
    if (timeoutUploadInput) {
        timeoutUploadInput.value = timeoutUpload;
    }

    updateSettingsUnsavedHints();
}

function loadAPILinkSettings() {
    if (!config) return;
    if (apiLinkDirty) {
        updateSettingsUnsavedHints();
        return;
    }
    const api = config.api || {};
    const enabledEl = document.getElementById('apiEnabled');
    const keyEl = document.getElementById('apiKey');
    const baseEl = document.getElementById('apiBaseUrl');
    const hintEl = document.getElementById('apiKeyHint');
    if (enabledEl) enabledEl.checked = Boolean(api.enabled);
    if (keyEl) {
        keyEl.value = '';
        keyEl.placeholder = api.key_set
            ? `Current: ${api.key_hint || 'awxb_...'} (leave blank to keep)`
            : 'Paste awxb_... key';
    }
    if (baseEl) {
        baseEl.value = api.base_url || '';
    }
    if (hintEl) {
        hintEl.textContent = api.key_set
            ? `Key on disk: ${api.key_hint}. Leave the field blank when saving to keep it.`
            : 'Paste a key from aviationwx.org ops. Leave blank when saving to keep the current key.';
    }
    if (api.base_url && api.base_url !== 'https://api.aviationwx.org') {
        const details = document.getElementById('apiAdvancedDetails');
        if (details) details.open = true;
    }
    updateSettingsUnsavedHints();
}

function updateAPILinkStatusPanel(apiLink) {
    const panel = document.getElementById('apiLinkStatusPanel');
    if (!panel) return;
    if (!apiLink) {
        panel.innerHTML = '<p class="form-help">No API link status yet (enable and save a key to start heartbeats).</p>';
        return;
    }
    const rows = [];
    rows.push(`<div class="info-row"><span>Configured</span><code>${apiLink.configured ? 'yes' : 'no'}</code></div>`);
    rows.push(`<div class="info-row"><span>Link status</span><code>${apiLink.status || '--'}</code></div>`);
    if (apiLink.airport_id) {
        rows.push(`<div class="info-row"><span>Airport</span><code>${apiLink.airport_id}${apiLink.airport_name ? ' - ' + apiLink.airport_name : ''}</code></div>`);
    }
    if (apiLink.bridge_id) {
        rows.push(`<div class="info-row"><span>Bridge ID</span><code>${apiLink.bridge_id}</code></div>`);
    }
    if (typeof apiLink.declination_deg === 'number') {
        const dir = apiLink.declination_deg >= 0 ? 'E' : 'W';
        rows.push(`<div class="info-row"><span>Declination</span><code>${Math.abs(apiLink.declination_deg).toFixed(1)}°${dir} (east-positive ${apiLink.declination_deg})</code></div>`);
    }
    if (apiLink.last_health_ok) {
        rows.push(`<div class="info-row"><span>Last health OK</span><code>${apiLink.last_health_ok}</code></div>`);
    }
    if (apiLink.last_error) {
        rows.push(`<div class="info-row"><span>Last error</span><code>${apiLink.last_error}</code></div>`);
    }
    panel.innerHTML = rows.join('');
}

async function saveAPILinkSettings() {
    const enabled = document.getElementById('apiEnabled')?.checked === true;
    const key = document.getElementById('apiKey')?.value?.trim() || '';
    const baseUrl = document.getElementById('apiBaseUrl')?.value?.trim() || '';
    if (enabled && key && key.length > 0) {
        // Client-side shape check mirrors core (awxb_ + 48 alnum)
        if (!/^awxb_[A-Za-z0-9]{48}$/.test(key)) {
            alert('API key must be awxb_ followed by exactly 48 letters or digits.');
            return;
        }
    }
    if (enabled && !key && !(config && config.api && config.api.key_set)) {
        alert('Paste an API key before enabling the link, or disable the link.');
        return;
    }
    try {
        const body = {
            api: {
                enabled,
                base_url: baseUrl,
            },
        };
        if (key) {
            body.api.key = key;
        }
        await api('/config', {
            method: 'PUT',
            body: JSON.stringify(body),
        });
        apiLinkDirty = false;
        document.getElementById('apiKey').value = '';
        showNotification('AviationWX link settings saved.', 'success');
        await loadConfig();
        loadAPILinkSettings();
    } catch (err) {
        alert('Failed to save API link: ' + err.message);
    }
}

async function testAPIBootstrap() {
    const resultDiv = document.getElementById('apiLinkTestResult');
    if (resultDiv) resultDiv.innerHTML = '<div class="test-result">Testing bootstrap...</div>';
    const key = document.getElementById('apiKey')?.value?.trim() || '';
    const baseUrl = document.getElementById('apiBaseUrl')?.value?.trim() || '';
    try {
        const result = await api('/test/api-bootstrap', {
            method: 'POST',
            body: JSON.stringify({ key, base_url: baseUrl }),
        });
        if (result.status === 'ok' && result.bootstrap) {
            const b = result.bootstrap;
            const decl = typeof b.declination_deg === 'number'
                ? `${b.declination_deg}° (${b.declination_source || 'n/a'})`
                : '--';
            if (resultDiv) {
                resultDiv.innerHTML = `<div class="test-result success">Bootstrap OK: ${b.airport_id || '?'} / ${b.bridge_id || '?'} - declination ${decl}</div>`;
            }
        } else {
            if (resultDiv) {
                resultDiv.innerHTML = `<div class="test-result error">${result.error || 'Bootstrap failed'}</div>`;
            }
        }
    } catch (err) {
        if (resultDiv) {
            resultDiv.innerHTML = `<div class="test-result error">${err.message}</div>`;
        }
    }
}

async function testAPIHealth() {
    const resultDiv = document.getElementById('apiLinkTestResult');
    if (resultDiv) resultDiv.innerHTML = '<div class="test-result">Testing link...</div>';
    try {
        const result = await api('/test/api-health', {
            method: 'POST',
            body: JSON.stringify({}),
        });
        if (result.status === 'ok' && result.result) {
            const r = result.result;
            if (resultDiv) {
                resultDiv.innerHTML = `<div class="test-result success">Link OK: ${r.airport_id || '?'} / ${r.bridge_id || '?'} (${r.link_status || 'operational'})</div>`;
            }
        } else {
            if (resultDiv) {
                resultDiv.innerHTML = `<div class="test-result error">${result.error || 'Link test failed'}</div>`;
            }
        }
    } catch (err) {
        if (resultDiv) {
            resultDiv.innerHTML = `<div class="test-result error">${err.message}</div>`;
        }
    }
}

function uploadSshStatusLabel(status) {
    switch (status) {
        case 'ok':
            return 'OK';
        case 'mismatch':
            return 'Mismatch';
        case 'mismatch_pending_heal':
            return 'Mismatch (trusted roster)';
        case 'roster_not_synced':
            return 'Roster not synced';
        case 'roster_sync_failed':
            return 'Roster sync failed';
        case 'probe_failed':
            return 'Probe failed';
        case 'pinned_key_error':
            return 'Pinned key unreadable';
        default:
            return status || 'Unknown';
    }
}

function uploadSshStatusClass(status) {
    switch (status) {
        case 'ok':
            return 'status-ok';
        case 'mismatch':
        case 'mismatch_pending_heal':
            return 'status-warn';
        case 'roster_not_synced':
        case 'roster_sync_failed':
        case 'probe_failed':
        case 'pinned_key_error':
            return 'status-error';
        default:
            return '';
    }
}

function formatTrustedUpdatedAt(value) {
    if (!value) return '—';
    try {
        return new Date(value).toLocaleString();
    } catch (_) {
        return value;
    }
}

function renderUploadSshKeys(endpoints) {
    const panel = document.getElementById('uploadSshKeysPanel');
    if (!panel) return;
    if (!endpoints || endpoints.length === 0) {
        panel.innerHTML = '<p class="form-help">No upload targets configured.</p>';
        return;
    }

    panel.innerHTML = endpoints.map((ep) => {
        const target = escapeHtml(`${ep.host}:${ep.port}`);
        const statusClass = uploadSshStatusClass(ep.status);
        const statusLabel = escapeHtml(uploadSshStatusLabel(ep.status));
        const serverKey = ep.server_key_sha256
            ? escapeHtml(ep.server_key_sha256)
            : (ep.server_key_error ? escapeHtml(`Error: ${ep.server_key_error}`) : '—');
        const pinnedKey = ep.pinned_key_sha256
            ? escapeHtml(ep.pinned_key_sha256)
            : (ep.pinned_key_error ? escapeHtml(`Error: ${ep.pinned_key_error}`) : '—');
        const roster = (ep.trusted_roster_sha256 && ep.trusted_roster_sha256.length)
            ? escapeHtml(ep.trusted_roster_sha256.join(', '))
            : '—';
        const source = ep.trusted_source
            ? escapeHtml(`${ep.trusted_source} (${formatTrustedUpdatedAt(ep.trusted_updated_at)})`)
            : '—';
        const rosterURL = ep.https_roster_url ? escapeHtml(ep.https_roster_url) : '—';
        const rosterSyncError = ep.roster_sync_error
            ? `<div class="info-row">
                    <span>Roster sync</span>
                    <span style="color:var(--color-danger);">${escapeHtml(ep.roster_sync_error)}</span>
                </div>`
            : '';
        return `
            <div class="upload-ssh-endpoint">
                <div class="info-row">
                    <span>Target</span>
                    <code>${target}</code>
                </div>
                <div class="info-row">
                    <span>Status</span>
                    <span class="upload-ssh-status ${statusClass}">${statusLabel}</span>
                </div>
                <div class="info-row">
                    <span>Server key (live)</span>
                    <code>${serverKey}</code>
                </div>
                <div class="info-row">
                    <span>Pinned key</span>
                    <code>${pinnedKey}</code>
                </div>
                <div class="info-row">
                    <span>Trusted roster</span>
                    <code>${roster}</code>
                </div>
                <div class="info-row">
                    <span>Roster source</span>
                    <span>${source}</span>
                </div>
                <div class="info-row">
                    <span>HTTPS roster URL</span>
                    <code>${rosterURL}</code>
                </div>
                ${rosterSyncError}
            </div>
        `;
    }).join('');
}

async function loadUploadSshKeys() {
    const panel = document.getElementById('uploadSshKeysPanel');
    if (!panel) return;
    panel.innerHTML = '<p class="form-help">Loading SSH host key status…</p>';
    try {
        const data = await api('/upload/ssh-host-keys');
        renderUploadSshKeys(data.endpoints || []);
    } catch (err) {
        console.error('Failed to load SSH host keys:', err);
        panel.innerHTML = `<p class="form-help" style="color:var(--color-danger);">Could not load SSH host key status: ${escapeHtml(err.message)}</p>`;
    }
}

async function refreshUploadSshKeys() {
    const panel = document.getElementById('uploadSshKeysPanel');
    if (!panel) return;
    panel.innerHTML = '<p class="form-help">Refreshing SSH host keys…</p>';
    try {
        const data = await api('/upload/ssh-host-keys', { method: 'POST' });
        renderUploadSshKeys(data.endpoints || []);
    } catch (err) {
        console.error('Failed to refresh SSH host keys:', err);
        panel.innerHTML = `<p class="form-help" style="color:var(--color-danger);">Could not refresh SSH host keys: ${escapeHtml(err.message)}</p>`;
    }
}

async function saveGlobalSettings() {
    const maxConcurrent = parseInt(document.getElementById('maxConcurrentUploads').value);
    const updateChannel = document.getElementById('updateChannel').value;
    const timeoutConnect = parseInt(document.getElementById('timeoutConnect').value);
    const timeoutUpload = parseInt(document.getElementById('timeoutUpload').value);
    
    if (maxConcurrent < 1 || maxConcurrent > 10) {
        alert('Concurrent uploads must be between 1 and 10');
        return;
    }

    const maxCapturesRaw = document.getElementById('maxConcurrentCaptures').value;
    const maxCaptures = maxCapturesRaw === '0' ? 0 : parseInt(maxCapturesRaw, 10);
    if (maxCaptures !== 0 && (maxCaptures < 1 || maxCaptures > 10 || Number.isNaN(maxCaptures))) {
        alert('Concurrent captures must be Auto (profiled) or between 1 and 10');
        return;
    }
    
    if (timeoutConnect < 10 || timeoutConnect > 300) {
        alert('Connection timeout must be between 10 and 300 seconds');
        return;
    }
    
    if (timeoutUpload < 60 || timeoutUpload > 600) {
        alert('Upload timeout must be between 60 and 600 seconds');
        return;
    }
    
    try {
        // Omit nested globals from the body: the server replaces several structs wholesale if present.
        await api('/config', {
            method: 'PUT',
            body: JSON.stringify({
                update_channel: updateChannel,
                max_concurrent_uploads: maxConcurrent,
                max_concurrent_captures: maxCaptures,
                timeout_connect_seconds: timeoutConnect,
                timeout_upload_seconds: timeoutUpload,
            }),
        });

        uploadSettingsDirty = false;
        updateSettingsUnsavedHints();

        showNotification(
            '✅ Settings saved. Upload/capture limits, SFTP timeouts, and update channel apply immediately. Restart the bridge only if you changed the web console listen port.',
            'success'
        );
        
        // Reload config to reflect changes
        await loadConfig();
    } catch (err) {
        alert('Failed to save settings: ' + err.message);
    }
}

// Setup Wizard + incomplete-config banners
const SETUP_DISMISSED_KEY = 'aviationwx_setup_dismissed';
let wizardStep = 'timezone';
let wizardResume = false;

function setupDismissed() {
    try {
        return sessionStorage.getItem(SETUP_DISMISSED_KEY) === '1';
    } catch {
        return false;
    }
}

function setSetupDismissed(v) {
    try {
        if (v) {
            sessionStorage.setItem(SETUP_DISMISSED_KEY, '1');
        } else {
            sessionStorage.removeItem(SETUP_DISMISSED_KEY);
        }
    } catch {
        // ignore
    }
}

function updateSetupBanner() {
    const el = document.getElementById('setupBanner');
    if (!el) return;
    const firstRun = window.isFirstRunSetup ? window.isFirstRunSetup(status) : Boolean(status?.first_run);
    el.style.display = firstRun && !setupDismissed() ? 'block' : 'none';
}

function dismissSetupBanner() {
    setSetupDismissed(true);
    updateSetupBanner();
}

function renderConfigBanners() {
    const host = document.getElementById('configBanners');
    if (!host) return;
    const select = window.selectConfigBanners;
    if (typeof select !== 'function') {
        host.innerHTML = '';
        return;
    }
    const banners = select({ config, status, cameras, stations }) || [];
    host.innerHTML = banners.map((b) => {
        const tone = b.tone === 'strong' ? 'banner-strong' : (b.tone === 'soft' ? 'banner-soft' : 'banner-info');
        return `<div class="banner ${tone}" data-banner-id="${escapeHtml(b.id)}">
            <div class="banner-content">
                <h3>${escapeHtml(b.title)}</h3>
                <p>${escapeHtml(b.body)}</p>
            </div>
        </div>`;
    }).join('');
}

function wizardFooter(primaryLabel, primaryFn, { showSkip = false, skipFn = null, showBack = false, backFn = null } = {}) {
    const back = showBack
        ? `<button type="button" class="btn" onclick="${backFn}">Back</button>`
        : `<button type="button" class="btn" onclick="closeModal()">Cancel</button>`;
    const skip = showSkip
        ? `<button type="button" class="btn" onclick="${skipFn}">Skip</button>`
        : '';
    return `<div style="display:flex;gap:var(--space-md);justify-content:flex-end;margin-top:var(--space-lg);flex-wrap:wrap;">
        ${back}
        ${skip}
        <button type="button" class="btn btn-primary" onclick="${primaryFn}">${primaryLabel}</button>
    </div>`;
}

function wizardGoTimezone() {
    wizardStep = 'timezone';
    renderWizardStep();
}

function wizardGoAPI() {
    wizardStep = 'api';
    renderWizardStep();
}

function wizardGoCameras() {
    wizardStep = 'cameras';
    renderWizardStep();
}

function showSetupWizard(resume = false) {
    wizardResume = Boolean(resume);
    setSetupDismissed(false);
    updateSetupBanner();
    if (wizardResume && typeof window.firstIncompleteWizardStep === 'function') {
        const next = window.firstIncompleteWizardStep({ config, cameras, stations });
        wizardStep = (next === 'api' || next === 'cameras' || next === 'weather' || next === 'done')
            ? next
            : 'timezone';
    } else {
        wizardStep = 'timezone';
    }
    renderWizardStep();
}

function renderWizardStep() {
    switch (wizardStep) {
        case 'api':
            renderWizardAPI();
            break;
        case 'cameras':
            renderWizardCameras();
            break;
        case 'weather':
            renderWizardWeather();
            break;
        case 'done':
            renderWizardDone();
            break;
        case 'timezone':
        default:
            renderWizardTimezone();
            break;
    }
}

function renderWizardTimezone() {
    const current = document.getElementById('timezone')?.value || status?.timezone || 'America/Los_Angeles';
    showModal('Setup - Timezone', `
        <div class="form-section">
            <p class="form-help" style="margin-bottom:var(--space-md)">Step 1 of 4. Timezone is required for camera timestamps. Later steps are optional.</p>
            <div class="form-group">
                <label for="wizardTimezone">Where are your cameras / stations located?</label>
                <select id="wizardTimezone" class="form-control">
                    ${TIMEZONES.map((tz) => `<option value="${tz.value}" ${tz.value === current ? 'selected' : ''}>${tz.label}</option>`).join('')}
                </select>
            </div>
        </div>
        ${wizardFooter('Continue', 'wizardSaveTimezone()')}
    `);
}

async function wizardSaveTimezone() {
    const timezone = document.getElementById('wizardTimezone')?.value;
    if (!timezone) return;
    try {
        await api('/time', {
            method: 'PUT',
            body: JSON.stringify({ timezone }),
        });
    } catch (err) {
        alert(err.message || String(err));
        return;
    }
    timezoneDirty = false;
    const tzMain = document.getElementById('timezone');
    if (tzMain) tzMain.value = timezone;
    updateSettingsUnsavedHints();
    await loadConfig();
    wizardStep = 'api';
    renderWizardStep();
}

function renderWizardAPI() {
    const hint = config?.api?.key_set
        ? `Key on disk: ${config.api.key_hint || 'awxb_...'}. Leave blank to keep it.`
        : 'Paste a key from aviationwx.org ops (awxb_ + 48 characters). Skip if waiting on ops.';
    showModal('Setup - AviationWX Link', `
        <div class="form-section">
            <p class="form-help" style="margin-bottom:var(--space-md)">Step 2 of 4 (skippable). Optional HTTPS link for fleet health and weather push. Cameras and SFTP work without a key.</p>
            <div class="form-group">
                <label>
                    <input type="checkbox" id="wizardApiEnabled" ${config?.api?.enabled ? 'checked' : ''}>
                    Enable AviationWX API link
                </label>
            </div>
            <div class="form-group">
                <label for="wizardApiKey">API key</label>
                <input type="password" id="wizardApiKey" class="form-control" autocomplete="off" spellcheck="false"
                       placeholder="Paste awxb_... key">
                <p class="form-help">${escapeHtml(hint)}</p>
            </div>
            <div id="wizardApiResult" class="camera-test-result"></div>
        </div>
        ${wizardFooter('Confirm & continue', 'wizardConfirmAPI()', {
            showSkip: true,
            skipFn: 'wizardSkipAPI()',
            showBack: true,
            backFn: 'wizardGoTimezone()',
        })}
    `);
}

async function wizardSkipAPI() {
    wizardStep = 'cameras';
    renderWizardStep();
}

async function wizardConfirmAPI() {
    const resultDiv = document.getElementById('wizardApiResult');
    const enabled = document.getElementById('wizardApiEnabled')?.checked === true;
    const key = document.getElementById('wizardApiKey')?.value?.trim() || '';
    if (enabled && key && !/^awxb_[A-Za-z0-9]{48}$/.test(key)) {
        if (resultDiv) {
            resultDiv.innerHTML = '<div class="test-result error">API key must be awxb_ followed by exactly 48 letters or digits.</div>';
        }
        return;
    }
    if (enabled && !key && !(config && config.api && config.api.key_set)) {
        if (resultDiv) {
            resultDiv.innerHTML = '<div class="test-result error">Paste an API key, or uncheck Enable, or Skip.</div>';
        }
        return;
    }
    try {
        const body = { api: { enabled } };
        if (key) body.api.key = key;
        await api('/config', { method: 'PUT', body: JSON.stringify(body) });
        apiLinkDirty = false;
        await loadConfig();
        await refreshStatus();
        const boot = await api('/test/api-bootstrap', {
            method: 'POST',
            body: JSON.stringify({ key: key || undefined, base_url: config?.api?.base_url || '' }),
        });
        const b = boot.result || boot;
        if (resultDiv) {
            const decl = typeof b.declination_deg === 'number'
                ? `${b.declination_deg}° (${b.declination_source || 'n/a'})`
                : 'n/a';
            resultDiv.innerHTML = `<div class="test-result success">Bootstrap OK: ${escapeHtml(b.airport_id || '?')} / ${escapeHtml(b.bridge_id || '?')} - declination ${escapeHtml(String(decl))} (display only; wind is true north)</div>`;
        }
        setTimeout(() => {
            wizardStep = 'cameras';
            renderWizardStep();
        }, 600);
    } catch (err) {
        if (resultDiv) {
            resultDiv.innerHTML = `<div class="test-result error">${escapeHtml(err.message || String(err))}</div>`;
        }
    }
}

function renderWizardCameras() {
    showModal('Setup - Cameras', `
        <div class="form-section">
            <p class="form-help" style="margin-bottom:var(--space-md)">Step 3 of 4 (skippable). Add an HTTP/RTSP/ONVIF camera with its own SFTP credentials, or skip for a weather-only site.</p>
            <p class="form-help">Configured cameras: <strong>${cameras.length}</strong></p>
        </div>
        ${wizardFooter('Add camera', 'wizardAddCamera()', {
            showSkip: true,
            skipFn: 'wizardSkipCameras()',
            showBack: true,
            backFn: 'wizardGoAPI()',
        })}
    `);
}

function wizardSkipCameras() {
    wizardStep = 'weather';
    renderWizardStep();
}

function wizardAddCamera() {
    closeModal();
    showSection('cameras');
    showAddCamera();
    // After closing camera modal the operator can Resume setup again.
}

function renderWizardWeather() {
    showModal('Setup - Weather', `
        <div class="form-section">
            <p class="form-help" style="margin-bottom:var(--space-md)">Step 4 of 4 (skippable). Add a Davis WeatherLink Live station (true-north vane install), or skip.</p>
            <p class="form-help">Configured stations: <strong>${stations.length}</strong></p>
        </div>
        ${wizardFooter('Add station', 'wizardAddStation()', {
            showSkip: true,
            skipFn: 'wizardFinish()',
            showBack: true,
            backFn: 'wizardGoCameras()',
        })}
    `);
}

function wizardAddStation() {
    closeModal();
    showSection('weather');
    showAddStation();
}

function wizardFinish() {
    setSetupDismissed(true);
    updateSetupBanner();
    renderConfigBanners();
    showModal('Setup complete', `
        <div class="form-section">
            <p style="margin-bottom:var(--space-md)">You can resume this flow anytime from Settings → AviationWX Link → Resume setup.</p>
            <p class="form-help">Wind direction assumes a true-north vane. Dashboard banners will nudge if weather is enabled without an API key.</p>
        </div>
        <div style="display:flex;justify-content:flex-end;margin-top:var(--space-lg);">
            <button type="button" class="btn btn-primary" onclick="closeModal()">Done</button>
        </div>
    `);
}

function renderWizardDone() {
    wizardFinish();
}

// Modal management
function showModal(title, content) {
    if (document.getElementById('cameraForm') && cameraFormDirty) {
        if (!confirm('Discard unsaved changes to this camera?')) {
            return;
        }
    }
    cameraFormDirty = false;
    document.getElementById('modalTitle').textContent = title;
    document.getElementById('modalBody').innerHTML = content;
    document.getElementById('modal').style.display = 'flex';
}

function markCameraFormDirtyIfNeeded(e) {
    if (!e.target.closest('#cameraForm')) return;
    cameraFormDirty = true;
}

function closeModal() {
    if (document.getElementById('cameraForm') && cameraFormDirty) {
        if (!confirm('Discard unsaved changes to this camera?')) {
            return;
        }
    }
    cameraFormDirty = false;
    if (lastCameraPreviewUrl) {
        URL.revokeObjectURL(lastCameraPreviewUrl);
        lastCameraPreviewUrl = null;
    }
    document.getElementById('modal').style.display = 'none';
}

// Utilities
function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

// Live Logs
let logsBuffer = [];
let logsPaused = false;
let maxLogLines = 500;

function startLiveLogs() {
    // Poll Docker logs every 2 seconds
    setInterval(async () => {
        if (logsPaused) return;
        
        try {
            const response = await fetchWithAuth('/api/logs?tail=100');
            
            if (response.ok) {
                const logs = await response.text();
                updateLogsDisplay(logs);
            }
        } catch (error) {
            console.error('Failed to fetch logs:', error);
        }
    }, 2000);
}

function updateLogsDisplay(newLogs) {
    if (!newLogs || logsPaused) return;
    
    const container = document.getElementById('logsContainer');
    if (!container) return;
    
    // Replace entire buffer with new logs (they're already newest-first from backend)
    const lines = newLogs.split('\n').filter(l => l.trim());
    logsBuffer = lines;
    
    // Keep only last maxLogLines
    if (logsBuffer.length > maxLogLines) {
        logsBuffer = logsBuffer.slice(-maxLogLines);
    }
    
    // Filter and format logs
    const filters = {
        ERROR: document.getElementById('filterError')?.checked ?? true,
        WARN: document.getElementById('filterWarn')?.checked ?? true,
        INFO: document.getElementById('filterInfo')?.checked ?? true,
        DEBUG: document.getElementById('filterDebug')?.checked ?? false
    };
    
    const html = logsBuffer
        .filter(line => {
            if (line.includes('level=ERROR') && !filters.ERROR) return false;
            if (line.includes('level=WARN') && !filters.WARN) return false;
            if (line.includes('level=INFO') && !filters.INFO) return false;
            if (line.includes('level=DEBUG') && !filters.DEBUG) return false;
            return true;
        })
        .map(line => formatLogLine(line))
        .join('\n');
    
    container.innerHTML = html || '<div style="color: #8b949e;">No logs matching filters</div>';
    
    // Auto-scroll to bottom if not paused
    if (!logsPaused) {
        container.scrollTop = container.scrollHeight;
    }
}

function formatLogLine(line) {
    // Color code by level
    let color = '#e6edf3'; // default
    if (line.includes('level=ERROR')) {
        color = '#f85149'; // red
    } else if (line.includes('level=WARN')) {
        color = '#d29922'; // yellow
    } else if (line.includes('level=INFO')) {
        color = '#58a6ff'; // blue
    } else if (line.includes('level=DEBUG')) {
        color = '#8b949e'; // muted
    }
    
    return `<div style="color: ${color}; margin-bottom: 0.25rem;">${escapeHtml(line)}</div>`;
}

function clearLogs() {
    logsBuffer = [];
    const container = document.getElementById('logsContainer');
    if (container) {
        container.innerHTML = '<div style="color: #8b949e;">Logs cleared. New logs will appear here...</div>';
    }
}

function pauseLogs() {
    logsPaused = !logsPaused;
    const btn = document.getElementById('pauseLogsText');
    if (btn) {
        btn.textContent = logsPaused ? 'Resume' : 'Pause';
    }
}

function updateLogFilters() {
    // Re-render with current buffer
    updateLogsDisplay('');
}

// Keyboard shortcuts
document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
        closeModal();
    }
});

// Periodic refresh
setInterval(async () => {
    await refreshStatus();
}, 30000);

// Smooth image refresh: only when a new capture exists (last_capture_time changed)
let imageRefreshIntervals = new Map();
let lastDisplayedCaptureTime = new Map();

function startSmoothImageRefresh() {
    imageRefreshIntervals.forEach((interval) => clearInterval(interval));
    imageRefreshIntervals.clear();

    const cameraIds = new Set();
    document.querySelectorAll('.camera-preview-img[data-camera-id]').forEach((img) => {
        const id = img.dataset.cameraId;
        if (id) cameraIds.add(id);
    });

    cameraIds.forEach((cameraId) => {
        const intervalId = setInterval(() => {
            smoothRefreshImage(cameraId);
        }, 5000);
        imageRefreshIntervals.set(cameraId, intervalId);
    });
}

function smoothRefreshImage(cameraId) {
    if (!window.shouldRefreshPreview?.(cameraId, lastDisplayedCaptureTime, status)) {
        return;
    }

    const camStats = status?.orchestrator?.camera_stats?.find((cs) => cs.camera_id === cameraId);
    const newCaptureTime = new Date(camStats.capture_stats.last_capture_time).getTime();

    loadAuthenticatedPreview(cameraId).then(() => {
        if (previewBlobURLs.has(cameraId)) {
            lastDisplayedCaptureTime.set(cameraId, newCaptureTime);
        }
    });
}

// Cleanup on page unload
window.addEventListener('beforeunload', () => {
    imageRefreshIntervals.forEach(interval => clearInterval(interval));
    previewBlobURLs.forEach((url) => URL.revokeObjectURL(url));
    previewBlobURLs.clear();
});

// Update Management
function showUpdateDialog(updateInfo) {
    const bodyFn = window.selfUpdateConfirmMessage;
    const body = typeof bodyFn === 'function'
        ? bodyFn(updateInfo)
        : `Latest version: ${updateInfo.latest_version}`;
    if (confirm(`Update Available\n\n${body}\n\nProceed with update?`)) {
        triggerUpdate();
    }
}

function showManualUpdateDialog(updateInfo) {
    const bodyFn = window.manualUpdateConfirmMessage;
    const body = typeof bodyFn === 'function'
        ? bodyFn(updateInfo)
        : `Version ${updateInfo.latest_version} is available. Update the container with your orchestration tooling.`;
    if (confirm(body)) {
        const urlFn = window.releaseNotesURL;
        const fallbackUrl = window.DEFAULT_RELEASE_NOTES_URL
            || 'https://github.com/alexwitherspoon/aviationwx.org-bridge/releases';
        const url = typeof urlFn === 'function' ? urlFn(updateInfo) : fallbackUrl;
        window.open(url, '_blank', 'noopener');
    }
}

async function triggerUpdate() {
    try {
        showNotification('Triggering update... This will take 1-2 minutes.', 'info');
        
        const result = await api('/update', {
            method: 'POST',
        });
        
        if (result.status === 'ok') {
            showNotification('Update triggered successfully! The bridge will restart shortly.', 'success');
            
            // Show countdown and wait for bridge to come back
            let countdown = 120; // 2 minutes
            const intervalId = setInterval(() => {
                countdown--;
                showNotification(`Waiting for bridge to restart... ${countdown}s remaining`, 'info');
                
                if (countdown <= 0) {
                    clearInterval(intervalId);
                    showNotification('Update complete! Refreshing page...', 'success');
                    setTimeout(() => location.reload(), 2000);
                }
            }, 1000);
        } else {
            showNotification(`Update failed: ${result.error || 'Unknown error'}`, 'error');
        }
    } catch (err) {
        showNotification(`Update trigger failed: ${err.message}`, 'error');
    }
}

/**
 * Toggle visibility for a password input paired with a Show/Hide button.
 * @param {string} inputId
 * @param {string} buttonId
 * @param {string} [fieldLabel] Human-readable field name for aria-label (e.g. "SFTP password").
 */
function togglePasswordField(inputId, buttonId, fieldLabel) {
    const input = document.getElementById(inputId);
    const btn = document.getElementById(buttonId);
    if (!input || !btn) return;
    const base = (fieldLabel && String(fieldLabel).trim()) || 'Password';
    if (input.type === 'password') {
        input.type = 'text';
        btn.textContent = 'Hide';
        btn.setAttribute('aria-pressed', 'true');
        btn.setAttribute('aria-label', `Hide ${base}`);
    } else {
        input.type = 'password';
        btn.textContent = 'Show';
        btn.setAttribute('aria-pressed', 'false');
        btn.setAttribute('aria-label', `Show ${base}`);
    }
}


