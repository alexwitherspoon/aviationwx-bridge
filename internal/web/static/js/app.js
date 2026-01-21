/**
 * AviationWX Bridge - Web Console
 * Frontend JavaScript application
 */

// State
let config = null;
let status = null;
let cameras = [];
let timeUpdateInterval = null;

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

async function loadCameras() {
    try {
        cameras = await api('/cameras');
        updateCameraList();
        updateCameraOverview();
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
    
    // Update timezone selector
    if (status.timezone) {
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
        <div class="camera-overview-item">
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
    
    // Start smooth image refresh for preview images
    startSmoothImageRefresh();
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
    
    container.innerHTML = cameras.map(cam => {
        // Calculate next capture countdown
        let nextCaptureText = 'Unknown';
        let captureStatusText = '';
        if (cam.worker_running && status && status.orchestrator) {
            const camStats = status.orchestrator.camera_stats?.find(cs => cs.camera_id === cam.id);
            if (camStats) {
                if (camStats.capture_stats?.currently_capturing) {
                    captureStatusText = '<span class="status-active">🔴 Capturing now</span>';
                } else if (camStats.capture_stats?.next_capture_time) {
                    const nextTime = new Date(camStats.capture_stats.next_capture_time);
                    const now = new Date();
                    const secondsUntil = Math.max(0, Math.floor((nextTime - now) / 1000));
                    nextCaptureText = `${secondsUntil}s`;
                    captureStatusText = `<span class="status-info">Next: ${nextCaptureText}</span>`;
                }
            }
        }

        // Upload status
        let uploadStatusText = '';
        if (status && status.orchestrator && status.orchestrator.upload_stats?.currently_uploading) {
            uploadStatusText = '<span class="status-active">🔴 Uploading</span>';
        }

        // Upload failures
        let uploadFailureText = '';
        if (status && status.orchestrator && status.orchestrator.upload_stats) {
            const perCameraFailures = status.orchestrator.upload_stats.per_camera_failures || {};
            const failures = perCameraFailures[cam.id] || 0;
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

        // Capture errors
        let captureErrorText = '';
        if (status && status.orchestrator) {
            const camStats = status.orchestrator.camera_stats?.find(cs => cs.camera_id === cam.id);
            if (camStats && camStats.last_error && camStats.last_error.Message) {
                const errorMsg = camStats.last_error.Message;
                // Extract the key error (Connection refused, etc.)
                let shortError = errorMsg;
                if (errorMsg.includes('Connection refused')) {
                    shortError = 'Connection refused - RTSP server not available';
                } else if (errorMsg.includes('timed out')) {
                    shortError = 'Connection timed out';
                } else if (errorMsg.includes('Authentication')) {
                    shortError = 'Authentication failed';
                } else {
                    // Extract just the last meaningful line
                    const lines = errorMsg.split('\n').filter(l => l.trim());
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

        // Queue status
        let queueCount = 0;
        let queueStatusClass = 'healthy';
        if (status && status.orchestrator) {
            const camStats = status.orchestrator.camera_stats?.find(cs => cs.camera_id === cam.id);
            if (camStats && camStats.queue_stats) {
                queueCount = camStats.queue_stats.image_count || 0;
                const queuePercent = (queueCount / 50) * 100; // Assume 50 is critical
                if (queuePercent > 80) queueStatusClass = 'critical';
                else if (queuePercent > 50) queueStatusClass = 'warning';
            }
        }

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
                <div class="camera-card-details">
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
                </div>
            </div>
        </div>
    `}).join('');
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

async function updateTimezone() {
    const timezone = document.getElementById('timezone').value;
    try {
        await api('/time', {
            method: 'PUT',
            body: JSON.stringify({ timezone }),
        });
        updateTimeDisplay();
        // Show success message with hot-reload confirmation
        showNotification('✅ Timezone updated! Workers reloaded automatically.', 'success');
    } catch (err) {
        alert('Failed to update timezone: ' + err.message);
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
                
                <div class="form-row">
                    <div class="form-group">
                        <label for="camId">Camera ID</label>
                        <input type="text" id="camId" class="form-control" 
                               value="${cam?.id || ''}" 
                               ${isEdit ? 'readonly' : 'required'}
                               placeholder="e.g., kord-west">
                        <p class="form-help">Unique identifier (lowercase, no spaces)</p>
                    </div>
                    <div class="form-group">
                        <label for="camName">Display Name</label>
                        <input type="text" id="camName" class="form-control" 
                               value="${cam?.name || ''}"
                               placeholder="e.g., KORD West Camera">
                    </div>
                </div>
                
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
                <div class="form-section-title">Upload Credentials</div>
                <p class="form-help" style="margin-bottom: var(--space-md)">
                    Contact <a href="mailto:contact@aviationwx.org">contact@aviationwx.org</a> to get upload credentials for your camera.
                </p>
                
                <div class="form-group">
                    <label for="uploadProtocol">Upload Protocol</label>
                    <select id="uploadProtocol" class="form-control" onchange="updateProtocolSettings()">
                        <option value="sftp" ${(cam?.upload?.protocol || 'sftp') === 'sftp' ? 'selected' : ''}>SFTP (Recommended)</option>
                        <option value="ftps" ${cam?.upload?.protocol === 'ftps' ? 'selected' : ''}>FTPS (Legacy)</option>
                    </select>
                    <p class="form-help">SFTP is more reliable on slow/unreliable connections</p>
                </div>
                
                <div class="form-row">
                    <div class="form-group">
                        <label for="uploadUser">Username</label>
                        <input type="text" id="uploadUser" class="form-control" 
                               value="${cam?.upload?.username || ''}"
                               required placeholder="your-username">
                    </div>
                    <div class="form-group">
                        <label for="uploadPass">Password</label>
                        <input type="password" id="uploadPass" class="form-control" 
                               ${isEdit ? 'placeholder="••••••••"' : 'required placeholder="your-password"'}>
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
                           value="${cam?.upload?.port || ((cam?.upload?.protocol || 'sftp') === 'sftp' ? 2222 : 2121)}"
                           min="1" max="65535" required>
                    <p class="form-help" id="uploadPortHelp">SFTP port (default: 2222)</p>
                </div>
                
                <div class="form-group" id="uploadBasePathGroup" style="display: ${(cam?.upload?.protocol || 'sftp') === 'sftp' ? 'block' : 'none'}">
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

function updateProtocolSettings() {
    const protocol = document.getElementById('uploadProtocol').value;
    const portInput = document.getElementById('uploadPort');
    const portHelp = document.getElementById('uploadPortHelp');
    const basePathGroup = document.getElementById('uploadBasePathGroup');
    
    if (protocol === 'sftp') {
        if (portInput.value == '21' || portInput.value == '2121') {
            portInput.value = '2222';
        }
        portHelp.textContent = 'SFTP port (default: 2222)';
        if (basePathGroup) basePathGroup.style.display = 'block';
    } else {
        if (portInput.value == '22' || portInput.value == '2222') {
            portInput.value = '2121';
        }
        portHelp.textContent = 'FTPS port (default: 2121)';
        if (basePathGroup) basePathGroup.style.display = 'none';
    }
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
    
    const type = document.getElementById('camType').value;
    const protocol = document.getElementById('uploadProtocol')?.value || 'sftp';
    const basePath = document.getElementById('uploadBasePath')?.value || '/files';
    const camera = {
        id: document.getElementById('camId').value.toLowerCase().replace(/\s+/g, '-'),
        name: document.getElementById('camName').value || document.getElementById('camId').value,
        type: type,
        enabled: document.getElementById('camEnabled').checked,
        capture_interval_seconds: parseInt(document.getElementById('camInterval').value, 10),
        upload: {
            protocol: protocol,
            host: document.getElementById('uploadHost').value || 'upload.aviationwx.org',
            port: parseInt(document.getElementById('uploadPort').value, 10) || (protocol === 'sftp' ? 2222 : 2121),
            username: document.getElementById('uploadUser').value,
            password: document.getElementById('uploadPass').value || undefined,
            tls: true,
            base_path: protocol === 'sftp' ? basePath : undefined,
        }
    };
    
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

async function testUpload() {
    const resultDiv = document.getElementById('uploadTestResult');
    resultDiv.innerHTML = '<div class="test-result" style="background: var(--color-bg)">Testing connection...</div>';
    
    const protocol = document.getElementById('uploadProtocol')?.value || 'sftp';
    const port = parseInt(document.getElementById('uploadPort').value) || (protocol === 'sftp' ? 2222 : 2121);
    
    try {
        const result = await api('/test/upload', {
            method: 'POST',
            body: JSON.stringify({
                protocol: protocol,
                host: document.getElementById('uploadHost').value || 'upload.aviationwx.org',
                port: port,
                username: document.getElementById('uploadUser').value,
                password: document.getElementById('uploadPass').value,
                tls: true,
            }),
        });
        
        if (result.status === 'ok') {
            resultDiv.innerHTML = `<div class="test-result success">✓ ${protocol.toUpperCase()} connection successful!</div>`;
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
        alert('Password updated successfully');
        document.getElementById('webPassword').value = '';
    } catch (err) {
        alert('Failed to save: ' + err.message);
    }
}

// Global Settings (concurrent uploads, update channel, timeouts)
async function loadGlobalSettings() {
    if (!config) return;
    
    // Load max concurrent uploads (from top-level, not nested in global)
    const maxConcurrent = config.max_concurrent_uploads || 2;
    const maxConcurrentSelect = document.getElementById('maxConcurrentUploads');
    if (maxConcurrentSelect) {
        maxConcurrentSelect.value = maxConcurrent.toString();
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
            timeout_connect_seconds: timeoutConnect,
            timeout_upload_seconds: timeoutUpload
        };
        
        await api('/config', {
            method: 'PUT',
            body: JSON.stringify(updatedConfig),
        });
        
        showNotification('✅ Settings saved successfully. Restart bridge to apply changes.', 'success');
        
        // Reload config to reflect changes
        await loadConfig();
    } catch (err) {
        alert('Failed to save settings: ' + err.message);
    }
}

// Setup Wizard
function showSetupWizard() {
    showModal('Welcome to AviationWX Bridge', `
        <div class="form-section">
            <p style="margin-bottom: var(--space-lg);">
                Let's set up your first camera! You'll need:
            </p>
            <ul style="margin-left: var(--space-lg); margin-bottom: var(--space-lg); color: var(--color-text-muted);">
                <li>Your camera's snapshot URL or RTSP stream address</li>
                <li>FTP credentials from aviationwx.org</li>
            </ul>
            <p style="margin-bottom: var(--space-lg);">
                Don't have FTP credentials yet? 
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

// Smooth image refresh system
let imageRefreshIntervals = new Map();

function startSmoothImageRefresh() {
    // Clear any existing intervals
    imageRefreshIntervals.forEach(interval => clearInterval(interval));
    imageRefreshIntervals.clear();
    
    // Find all camera preview images
    const previewImages = document.querySelectorAll('.camera-preview-img');
    
    previewImages.forEach(img => {
        const cameraId = img.dataset.cameraId;
        if (!cameraId) return;
        
        // Create smooth refresh for this camera
        // Refresh every 5 seconds (adjust based on your needs)
        const intervalId = setInterval(() => {
            smoothRefreshImage(img, cameraId);
        }, 5000);
        
        imageRefreshIntervals.set(cameraId, intervalId);
    });
}

function smoothRefreshImage(img, cameraId) {
    // Create a new image in the background
    const newImg = new Image();
    const refreshUrl = `/api/cameras/${cameraId}/preview?t=${Date.now()}`;
    
    newImg.onload = function() {
        // Fade out current image
        img.style.transition = 'opacity 0.3s ease-in-out';
        img.style.opacity = '0';
        
        // After fade out, swap the src and fade in
        setTimeout(() => {
            img.src = refreshUrl;
            img.style.opacity = '1';
        }, 300);
    };
    
    newImg.onerror = function() {
        // If new image fails to load, don't update
        console.log(`Failed to load preview for ${cameraId}`);
    };
    
    // Start loading the new image
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




