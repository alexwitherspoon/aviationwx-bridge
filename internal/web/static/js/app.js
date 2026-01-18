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
    
    document.getElementById('statCameras').textContent = status.camera_count || 0;
    document.getElementById('configVersion').textContent = status.version || '2';
    
    // Update timezone selector
    if (status.timezone) {
        document.getElementById('timezone').value = status.timezone;
    }
    
    // Update orchestrator stats if available
    if (status.orchestrator) {
        const orch = status.orchestrator;
        document.getElementById('statUploads').textContent = orch.uploads_today || 0;
        document.getElementById('statQueue').textContent = orch.queue_size || 0;
        document.getElementById('statNTP').textContent = orch.ntp_healthy ? 'OK' : 'WARN';
        
        // Update version display
        const versionEl = document.getElementById('appVersion');
        if (versionEl && orch.git_commit) {
            const version = orch.version || 'dev';
            const commit = orch.git_commit || 'unknown';
            versionEl.textContent = `${version}:${commit}`;
        }
        
        // Update availability indicator
        const updateEl = document.getElementById('updateAvailable');
        if (updateEl && orch.update) {
            if (orch.update.available) {
                updateEl.style.display = 'inline-block';
                updateEl.href = orch.update.latest_url || '#';
                updateEl.title = `Update available: ${orch.update.latest_version}`;
            } else {
                updateEl.style.display = 'none';
            }
        }
        
        // Update system resources display (includes queue storage)
        updateSystemResourcesDisplay(orch.system, orch.queue_storage);
    }
}

// System resources display
function updateSystemResourcesDisplay(system, queueStorage) {
    // CPU
    const cpuPercent = system?.cpu_percent || 0;
    const cpuLevel = system?.cpu_level || 'healthy';
    updateResourceBar('cpu', cpuPercent, cpuLevel, `${cpuPercent.toFixed(0)}%`);
    
    // Memory
    const memPercent = system?.mem_percent || 0;
    const memLevel = system?.mem_level || 'healthy';
    const memUsed = system?.mem_used_mb || 0;
    updateResourceBar('mem', memPercent, memLevel, `${memPercent.toFixed(0)}%`);
    
    // Queue storage
    const queuePercent = queueStorage?.capacity_percent || 0;
    const queueLevel = queuePercent >= 80 ? 'critical' : queuePercent >= 50 ? 'warning' : 'healthy';
    const queueImages = queueStorage?.total_images || 0;
    updateResourceBar('queue', queuePercent, queueLevel, `${queuePercent.toFixed(0)}%`);
    
    // Overall badge
    const overallLevel = system?.overall_level || 'healthy';
    const badgeEl = document.getElementById('systemOverallBadge');
    if (badgeEl) {
        badgeEl.classList.remove('healthy', 'warning', 'critical');
        badgeEl.classList.add(overallLevel);
        badgeEl.textContent = overallLevel === 'healthy' ? 'Healthy' : 
                              overallLevel === 'warning' ? 'Warning' : 'Critical';
    }
    
    // Details text
    const detailsEl = document.getElementById('resourceDetailsText');
    if (detailsEl) {
        const uptime = system?.uptime || '--';
        detailsEl.textContent = `CPU: ${cpuPercent.toFixed(0)}% • Memory: ${memUsed.toFixed(0)} MB • Queue: ${queueImages} images • Uptime: ${uptime}`;
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
    
    container.innerHTML = cameras.map(cam => `
        <div class="camera-overview-item">
            <div class="camera-preview">
                <img src="/api/cameras/${cam.id}/preview?t=${Date.now()}" 
                     alt="${escapeHtml(cam.name)}"
                     onerror="this.style.display='none'; this.nextElementSibling.style.display='flex'">
                <span style="display:none">No Preview</span>
            </div>
            <div class="camera-info">
                <div class="camera-name">${escapeHtml(cam.name)}</div>
                <div class="camera-meta">${cam.type} • ${cam.capture_interval_seconds}s interval</div>
            </div>
            <div class="camera-status">
                <span class="camera-status-badge ${cam.enabled ? 'active' : 'paused'}">
                    ${cam.enabled ? 'Active' : 'Paused'}
                </span>
            </div>
        </div>
    `).join('');
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
    
    container.innerHTML = cameras.map(cam => `
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
                    <img src="/api/cameras/${cam.id}/preview?t=${Date.now()}" 
                         alt="${escapeHtml(cam.name)}"
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
                        <span class="label">Upload User</span>
                        <span class="value">${cam.upload?.username || 'Not configured'}</span>
                    </div>
                    <div class="detail-row">
                        <span class="label">Upload Host</span>
                        <span class="value">${cam.upload?.host || 'upload.aviationwx.org'}</span>
                    </div>
                    
                    <div class="queue-health">
                        <div class="queue-health-label">
                            <span>Queue</span>
                            <span>0 files</span>
                        </div>
                        <div class="queue-health-bar">
                            <div class="queue-health-fill healthy" style="width: 0%"></div>
                        </div>
                    </div>
                </div>
            </div>
        </div>
    `).join('');
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
    } catch (err) {
        alert('Failed to update timezone: ' + err.message);
    }
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
                    Contact <a href="mailto:contact@aviationwx.org">contact@aviationwx.org</a> to get FTP credentials for your camera.
                </p>
                
                <div class="form-row">
                    <div class="form-group">
                        <label for="uploadUser">FTP Username</label>
                        <input type="text" id="uploadUser" class="form-control" 
                               value="${cam?.upload?.username || ''}"
                               required placeholder="your-ftp-username">
                    </div>
                    <div class="form-group">
                        <label for="uploadPass">FTP Password</label>
                        <input type="password" id="uploadPass" class="form-control" 
                               ${isEdit ? 'placeholder="••••••••"' : 'required placeholder="your-ftp-password"'}>
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
                           value="${cam?.upload?.port || 2121}"
                           min="1" max="65535" required>
                    <p class="form-help">FTPS port (default: 2121)</p>
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
    const camera = {
        id: document.getElementById('camId').value.toLowerCase().replace(/\s+/g, '-'),
        name: document.getElementById('camName').value || document.getElementById('camId').value,
        type: type,
        enabled: document.getElementById('camEnabled').checked,
        capture_interval_seconds: parseInt(document.getElementById('camInterval').value, 10),
        upload: {
            host: document.getElementById('uploadHost').value || 'upload.aviationwx.org',
            port: parseInt(document.getElementById('uploadPort').value, 10) || 2121,
            username: document.getElementById('uploadUser').value,
            password: document.getElementById('uploadPass').value || undefined,
            tls: true,
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
    
    try {
        const result = await api('/test/upload', {
            method: 'POST',
            body: JSON.stringify({
                host: document.getElementById('uploadHost').value || 'upload.aviationwx.org',
                port: 21,
                username: document.getElementById('uploadUser').value,
                password: document.getElementById('uploadPass').value,
                tls: true,
            }),
        });
        
        if (result.success) {
            resultDiv.innerHTML = '<div class="test-result success">✓ Connection successful!</div>';
        } else {
            resultDiv.innerHTML = `<div class="test-result error">✗ ${result.error}</div>`;
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




