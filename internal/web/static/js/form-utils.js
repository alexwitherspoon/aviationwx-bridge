/**
 * Pure form utilities for camera config. Testable without DOM.
 */

/**
 * buildCameraConfigFromFormValues builds a camera config object from form values.
 * Returns null if type is missing or required fields for the type are empty.
 * @param {Object} values - Form field values
 * @param {string} [values.type] - Camera type: "http", "rtsp", "onvif"
 * @param {string} [values.id] - Camera ID (default: "test")
 * @param {string} [values.snapshot_url] - HTTP snapshot URL
 * @param {string} [values.auth_user] - Basic auth username
 * @param {string} [values.auth_pass] - Basic auth password
 * @param {string} [values.rtsp_url] - RTSP URL
 * @param {string} [values.rtsp_user] - RTSP username
 * @param {string} [values.rtsp_pass] - RTSP password
 * @param {string} [values.onvif_endpoint] - ONVIF endpoint
 * @param {string} [values.onvif_user] - ONVIF username
 * @param {string} [values.onvif_pass] - ONVIF password
 * @param {string} [values.onvif_profile] - ONVIF profile token
 * @returns {Object|null} Camera config or null
 */
export function buildCameraConfigFromFormValues(values) {
    const type = values.type;
    if (!type) return null;

    const id = (values.id || 'test').toLowerCase().replace(/\s+/g, '-');
    const camera = { id, type };

    if (type === 'http') {
        const url = values.snapshot_url;
        if (!url) return null;
        camera.snapshot_url = url;
        const authUser = values.auth_user;
        const authPass = values.auth_pass;
        if (authUser) {
            camera.auth = { type: 'basic', username: authUser, password: authPass || '' };
        }
    } else if (type === 'rtsp') {
        const url = values.rtsp_url;
        if (!url) return null;
        camera.rtsp = {
            url,
            username: values.rtsp_user,
            password: values.rtsp_pass,
        };
    } else if (type === 'onvif') {
        const endpoint = values.onvif_endpoint;
        if (!endpoint) return null;
        camera.onvif = {
            endpoint,
            username: values.onvif_user,
            password: values.onvif_pass,
            profile_token: values.onvif_profile || undefined,
        };
    } else {
        return null;
    }

    return camera;
}

/**
 * Normalized SFTP identity key (host:port:username) for duplicate detection. Matches server-side rules.
 * @param {string|undefined} host
 * @param {number|string|undefined} port
 * @param {string|undefined} username
 * @returns {string} Empty if host or username is blank.
 */
export function uploadCredentialKey(host, port, username) {
    const h = String(host ?? '').trim().toLowerCase();
    let p = Number.parseInt(String(port), 10);
    if (!Number.isFinite(p) || p <= 0) {
        p = 2222;
    }
    const u = String(username ?? '').trim();
    if (!h || !u) {
        return '';
    }
    return `${h}:${p}:${u}`;
}

/**
 * Derives a camera id from display name (matches server SlugCameraIDFromName).
 * @param {string|undefined} name
 * @returns {string}
 */
export function slugCameraIdFromName(name) {
    const s = String(name ?? '').trim().toLowerCase();
    if (!s) return '';
    const parts = [];
    let cur = '';
    const flush = () => {
        if (cur) {
            parts.push(cur);
            cur = '';
        }
    };
    for (const ch of s) {
        const r = ch.codePointAt(0);
        const isAlnum = (r >= 97 && r <= 122) || (r >= 48 && r <= 57);
        if (isAlnum) {
            cur += ch;
        } else {
            flush();
        }
    }
    flush();
    let out = parts.join('-');
    if (out.length > 64) {
        out = out.slice(0, 64).replace(/-+$/g, '');
    }
    return out;
}

/**
 * Returns the id of another camera using the same SFTP identity, or null.
 * @param {Array<{id: string, upload?: {host?: string, port?: number, username?: string}}>} cameraList
 * @param {string|null|undefined} excludeId - camera id being edited (skipped)
 * @param {string|undefined} host
 * @param {number|string|undefined} port
 * @param {string|undefined} username
 * @returns {string|null}
 */
export function findConflictingCameraId(cameraList, excludeId, host, port, username) {
    const k = uploadCredentialKey(host, port, username);
    if (!k) {
        return null;
    }
    for (const cam of cameraList) {
        if (excludeId && cam.id === excludeId) {
            continue;
        }
        const up = cam.upload;
        if (!up) {
            continue;
        }
        if (uploadCredentialKey(up.host, up.port, up.username) === k) {
            return cam.id;
        }
    }
    return null;
}

/**
 * encodeBasicAuthHeader returns an HTTP Basic Authorization header value.
 * Uses UTF-8 so passwords outside Latin-1 work (btoa alone throws on Unicode).
 * @param {string} username
 * @param {string} password
 * @returns {string}
 */
export function encodeBasicAuthHeader(username, password) {
    const credentials = `${username}:${password}`;
    const bytes = new TextEncoder().encode(credentials);
    let binary = '';
    for (let i = 0; i < bytes.length; i++) {
        binary += String.fromCharCode(bytes[i]);
    }
    return 'Basic ' + btoa(binary);
}
