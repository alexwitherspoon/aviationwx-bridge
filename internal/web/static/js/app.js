/**
 * AviationWX.org Bridge - Web Console
 * Frontend JavaScript application
 */

// State
let config = null;
let status = null;
let previousCameraIds = null;
let cameras = [];
let timeUpdateInterval = null;
/** Unsaved local edits — skip syncing those form sections from the server until Save. */
let timezoneDirty = false;
let webConsoleDirty = false;
let uploadSettingsDirty = false;

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

// Initialize application
document.addEventListener('DOMContentLoaded', async () => {
    setupNavigation();
    populateTimezones();
    await refreshStatus();
    await loadConfig();
    await loadCameras();
    startTimeUpdates();
    startAutoRefresh(); // Auto-refresh dashboard every second
    startLiveLogs();    // Start live log streaming
    
    // Check for first run
    if (status && status.first_run) {
        document.getElementById('setupBanner').style.display = 'block';
    }
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
    }
    
    // Update URL
    window.location.hash = sectionId;
}

// API calls
async function api(endpoint, options = {}) {
    const url = `/api${endpoint}`;
    const response = await fetch(url, {
        ...options,
        credentials: 'same-origin',
        headers: {
            'Content-Type': 'application/json',
            ...options.headers,
        },
    });
    
    if (!response.ok) {
        const error = await response.text();
        throw new Error(error || `HTTP ${response.status}`);
    }
    
    return response.json();
}

async function refreshStatus() {
    try {
        status = await api('/status');
        updateStatusDisplay();
        document.getElementById('statusDot').classList.add('connected');
        document.getElementById('statusText').textContent = 'Connected';
    } catch (err) {
        console.error('Failed to fetch status:', err);
        document.getElementById('statusDot').classList.add('error');
        document.getElementById('statusText').textContent = 'Disconnected';
    }
}

/** Loads global config from the API (required for settings UI and PUT /config spreads). */
async function loadConfig() {
    try {
        config = await api('/config');
        loadGlobalSettings();
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
        } else {
            updateCameraListStatus();
            updateCameraOverviewStatus();
        }
    } catch (err) {
        console.error('Failed to load cameras:', err);
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
    }
    
    // Update available notification
    if (status.update && status.update.update_available) {
        const updateLink = document.getElementById('updateAvailable');
        updateLink.style.display = 'inline-block';
        updateLink.textContent = `⬆ Update to ${status.update.latest_version}`;
        updateLink.onclick = (e) => {
            e.preventDefault();
            showUpdateDialog(status.update);
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

// Legacy queue storage display (for backwards compatibility)
function updateQueueStorageDisplay(storage) {
    // This is now handled by updateSystemResourcesDisplay
    // Keeping for any direct calls
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
                <img src="/api/cameras/${cam.id}/preview" 
                     alt="${escapeHtml(cam.name)}"
                     class="camera-preview-img"
                     data-camera-id="${cam.id}"
                     onerror="this.style.display='none'; this.nextElementSibling.style.display='flex'">
                <span style="display:none">No Preview</span>
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
                    <img src="/api/cameras/${cam.id}/preview" 
                         alt="${escapeHtml(cam.name)}"
                         class="camera-preview-img"
                         data-camera-id="${cam.id}"
                         onerror="this.style.display='none'; this.nextElementSibling.style.display='flex'">
                    <span style="display:none">Preview not available</span>
                </div>
                <div class="camera-card-details" data-camera-id="${cam.id}">${detailsHtml}</div>
            </div>
        </div>
    `}).join('');
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

function startAutoRefresh() {
    // Refresh status and cameras every second for live countdown/status updates
    setInterval(async () => {
        try {
            await refreshStatus();
            
            // Only refresh camera list if on dashboard or cameras page
            const activeSection = document.querySelector('.section.active');
            if (activeSection && (activeSection.id === 'dashboard' || activeSection.id === 'cameras')) {
                await loadCameras();
            }
        } catch (error) {
            console.error('Auto-refresh error:', error);
        }
    }, 1000);
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

async function saveTimezone() {
    const timezone = document.getElementById('timezone').value;
    try {
        await api('/time', {
            method: 'PUT',
            body: JSON.stringify({ timezone }),
        });
        timezoneDirty = false;
        updateSettingsUnsavedHints();
        updateTimeDisplay();
        showNotification('✅ Timezone updated! Workers reloaded automatically.', 'success');
    } catch (err) {
        alert('Failed to save timezone: ' + err.message);
    }
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
        const response = await fetch('/api/test/camera', {
            method: 'POST',
            credentials: 'same-origin',
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
    
    try {
        await api('/config', {
            method: 'PUT',
            body: JSON.stringify({
                ...config,
                web_console: {
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
    if (uploadSettingsDirty) {
        updateSettingsUnsavedHints();
        return;
    }

    // Load max concurrent uploads (from top-level, not nested in global)
    const maxConcurrent = config.max_concurrent_uploads || 2;
    const maxConcurrentSelect = document.getElementById('maxConcurrentUploads');
    if (maxConcurrentSelect) {
        maxConcurrentSelect.value = maxConcurrent.toString();
    }

    const maxCaptures = (typeof config.max_concurrent_captures === 'number' && config.max_concurrent_captures >= 1 && config.max_concurrent_captures <= 10)
        ? config.max_concurrent_captures
        : 2;
    const maxCapturesSelect = document.getElementById('maxConcurrentCaptures');
    if (maxCapturesSelect) {
        maxCapturesSelect.value = maxCaptures.toString();
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

async function saveGlobalSettings() {
    const maxConcurrent = parseInt(document.getElementById('maxConcurrentUploads').value);
    const maxCaptures = parseInt(document.getElementById('maxConcurrentCaptures').value);
    const updateChannel = document.getElementById('updateChannel').value;
    const timeoutConnect = parseInt(document.getElementById('timeoutConnect').value);
    const timeoutUpload = parseInt(document.getElementById('timeoutUpload').value);
    
    if (maxConcurrent < 1 || maxConcurrent > 10) {
        alert('Concurrent uploads must be between 1 and 10');
        return;
    }

    if (maxCaptures < 1 || maxCaptures > 10) {
        alert('Concurrent captures must be between 1 and 10');
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
        // Update config with new global settings
        const updatedConfig = {
            ...config,
            update_channel: updateChannel,
            max_concurrent_uploads: maxConcurrent,
            max_concurrent_captures: maxCaptures,
            timeout_connect_seconds: timeoutConnect,
            timeout_upload_seconds: timeoutUpload
        };
        
        await api('/config', {
            method: 'PUT',
            body: JSON.stringify(updatedConfig),
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

// Setup Wizard
function showSetupWizard() {
    showModal('Welcome to AviationWX.org Bridge', `
        <div class="form-section">
            <p style="margin-bottom: var(--space-lg);">
                Let's set up your first camera! You'll need:
            </p>
            <ul style="margin-left: var(--space-lg); margin-bottom: var(--space-lg); color: var(--color-text-muted);">
                <li>Your camera's snapshot URL or RTSP stream address</li>
                <li>SFTP credentials from aviationwx.org</li>
            </ul>
            <p style="margin-bottom: var(--space-lg);">
                Don't have SFTP credentials yet? 
                Contact <a href="mailto:contact@aviationwx.org">contact@aviationwx.org</a> to get them.
            </p>
        </div>
        
        <div class="form-section">
            <div class="form-section-title">Step 1: Set Your Timezone</div>
            <div class="form-group">
                <label for="wizardTimezone">Where are your cameras located?</label>
                <select id="wizardTimezone" class="form-control">
                    ${TIMEZONES.map(tz => `<option value="${tz.value}">${tz.label}</option>`).join('')}
                </select>
            </div>
        </div>
        
        <div style="display: flex; gap: var(--space-md); justify-content: flex-end; margin-top: var(--space-lg);">
            <button type="button" class="btn" onclick="closeModal()">Cancel</button>
            <button type="button" class="btn btn-primary" onclick="wizardStep2()">Continue →</button>
        </div>
    `);
}

async function wizardStep2() {
    // Save timezone
    const timezone = document.getElementById('wizardTimezone').value;
    try {
        await api('/time', {
            method: 'PUT',
            body: JSON.stringify({ timezone }),
        });
        timezoneDirty = false;
        const tzMain = document.getElementById('timezone');
        if (tzMain) tzMain.value = timezone;
        updateSettingsUnsavedHints();
    } catch (err) {
        console.error('Failed to set timezone:', err);
    }
    
    // Show add camera form
    closeModal();
    showSection('cameras');
    showAddCamera();
    document.getElementById('setupBanner').style.display = 'none';
}

// Modal management
function showModal(title, content) {
    document.getElementById('modalTitle').textContent = title;
    document.getElementById('modalBody').innerHTML = content;
    document.getElementById('modal').style.display = 'flex';
}

function closeModal() {
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
            const response = await fetch('/api/logs?tail=100', {
                credentials: 'same-origin'
            });
            
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

    const refreshUrl = `/api/cameras/${cameraId}/preview?t=${Date.now()}`;
    const newImg = new Image();

    newImg.onload = function () {
        lastDisplayedCaptureTime.set(cameraId, newCaptureTime);
        const imgs = document.querySelectorAll(`.camera-preview-img[data-camera-id="${cameraId}"]`);
        imgs.forEach((img) => {
            img.style.transition = 'opacity 0.5s ease-in-out';
            img.style.opacity = '0';

            setTimeout(() => {
                img.src = refreshUrl;
                img.style.opacity = '1';
            }, 500);
        });
    };

    newImg.onerror = function () {
        console.log(`Failed to load preview for ${cameraId}`);
    };

    newImg.src = refreshUrl;
}

// Cleanup on page unload
window.addEventListener('beforeunload', () => {
    imageRefreshIntervals.forEach(interval => clearInterval(interval));
});

// Update Management
function showUpdateDialog(updateInfo) {
    const message = `
        <div style="text-align: left; padding: 1rem;">
            <p><strong>Current Version:</strong> ${updateInfo.current_version}</p>
            <p><strong>Latest Version:</strong> ${updateInfo.latest_version}</p>
            <p style="margin-top: 1.5rem;">This will trigger the supervisor script to pull and restart with the new version.</p>
            <p><strong>⚠️ Warning:</strong> The bridge will be unavailable for 1-2 minutes during the update.</p>
        </div>
    `;
    
    if (confirm(`Update Available\n\n${message.replace(/<[^>]*>/g, '')}\n\nProceed with update?`)) {
        triggerUpdate();
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


