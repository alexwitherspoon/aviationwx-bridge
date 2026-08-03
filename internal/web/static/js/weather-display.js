/**
 * Pure helpers for Weather page display (LAN age + raw payload) and Discover SSE.
 * Testable without DOM.
 */

/**
 * formatObservationAge returns a short age string from an RFC3339 observed_at.
 * @param {string|undefined|null} observedAtISO
 * @param {number} [nowMs]
 * @returns {string}
 */
export function formatObservationAge(observedAtISO, nowMs = Date.now()) {
    if (!observedAtISO) {
        return 'never';
    }
    const t = Date.parse(observedAtISO);
    if (Number.isNaN(t)) {
        return 'unknown';
    }
    const sec = Math.max(0, Math.floor((nowMs - t) / 1000));
    if (sec < 60) {
        return `${sec}s ago`;
    }
    const min = Math.floor(sec / 60);
    if (min < 60) {
        return `${min}m ago`;
    }
    const hr = Math.floor(min / 60);
    if (hr < 48) {
        return `${hr}h ago`;
    }
    const days = Math.floor(hr / 24);
    return `${days}d ago`;
}

/**
 * formatRawPayload pretty-prints station-native JSON for the payload log.
 * @param {unknown} raw
 * @returns {string}
 */
export function formatRawPayload(raw) {
    if (raw === undefined || raw === null) {
        return '';
    }
    try {
        return JSON.stringify(raw, null, 2);
    } catch {
        return String(raw);
    }
}

/**
 * consumeSSEBuffer parses complete SSE frames from a text buffer.
 * Returns parsed JSON objects from data: lines and any leftover incomplete frame.
 * @param {string} buffer
 * @returns {{ events: object[], rest: string }}
 */
export function consumeSSEBuffer(buffer) {
    const events = [];
    let rest = buffer;
    while (true) {
        const sep = rest.indexOf('\n\n');
        if (sep < 0) {
            break;
        }
        const frame = rest.slice(0, sep);
        rest = rest.slice(sep + 2);
        const dataLines = [];
        for (const line of frame.split('\n')) {
            if (line.startsWith('data:')) {
                dataLines.push(line.slice(5).replace(/^ /, ''));
            }
        }
        if (dataLines.length === 0) {
            continue;
        }
        try {
            events.push(JSON.parse(dataLines.join('\n')));
        } catch {
            // Malformed frame: skip.
        }
    }
    return { events, rest };
}

/**
 * discoverHostValue builds the Host field value from a discover candidate.
 * @param {{ host?: string, port?: number }} c
 * @returns {string}
 */
export function discoverHostValue(c) {
    if (!c || !c.host) {
        return '';
    }
    if (c.port && c.port !== 80) {
        return `${c.host}:${c.port}`;
    }
    return c.host;
}
