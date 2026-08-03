/**
 * Setup wizard + incomplete-config banner helpers (testable without DOM).
 */

/**
 * @typedef {'timezone'|'api'|'cameras'|'weather'|'done'} WizardStep
 */

/**
 * @typedef {object} BannerSpec
 * @property {string} id
 * @property {'strong'|'info'|'soft'} tone
 * @property {string} title
 * @property {string} body
 */

/**
 * selectConfigBanners returns dashboard banners for incomplete / waiting states.
 * Does not include first-run welcome (handled separately).
 *
 * @param {object} input
 * @param {object} [input.config]
 * @param {object} [input.status]
 * @param {Array<{enabled?: boolean, id?: string}>} [input.cameras]
 * @param {Array<{enabled?: boolean, id?: string}>} [input.stations]
 * @returns {BannerSpec[]}
 */
export function selectConfigBanners({ config = {}, status = {}, cameras = [], stations = [] } = {}) {
    const banners = [];
    const apiCfg = config.api || {};
    const apiLink = status.api_link || {};
    const apiConfigured = Boolean(apiLink.configured) || Boolean(apiCfg.enabled && apiCfg.key_set);
    const enabledStations = (stations || []).filter((s) => s && s.enabled !== false);
    const enabledCameras = (cameras || []).filter((c) => c && c.enabled !== false);
    const hasAnySource = enabledCameras.length > 0 || enabledStations.length > 0;

    if (enabledStations.length > 0 && !apiConfigured) {
        banners.push({
            id: 'weather_no_key',
            tone: 'strong',
            title: 'Weather stations need an AviationWX API key',
            body: 'LAN poll can run without a key, but observations will not reach aviationwx.org until you paste a key under Settings → AviationWX Link. You may be waiting on ops for a key.',
        });
    }

    if (apiConfigured) {
        const st = String(apiLink.status || '').toLowerCase();
        const failed = st === 'down' || (apiLink.last_error && st !== 'operational');
        if (failed) {
            banners.push({
                id: 'api_link_failed',
                tone: 'strong',
                title: 'AviationWX API link is not healthy',
                body: apiLink.last_error
                    ? `Fix or re-enter the API key. Last error: ${apiLink.last_error}`
                    : 'Fix or re-enter the API key, then use Test bootstrap / Test link.',
            });
        }
    }

    if (apiConfigured && !hasAnySource) {
        banners.push({
            id: 'no_sources',
            tone: 'soft',
            title: 'No cameras or weather stations yet',
            body: 'Add a camera under Cameras or a station under Weather when you are ready.',
        });
    }

    const enabledSources = apiLink.enabled_sources;
    if (apiConfigured && Array.isArray(enabledSources) && enabledStations.length > 0) {
        const enabledIDs = new Set(
            enabledSources
                .filter((s) => s && s.enabled !== false)
                .map((s) => s.bridge_source_id || s.bridgeSourceId)
                .filter(Boolean)
        );
        const pending = enabledStations.filter((s) => s.id && !enabledIDs.has(s.id));
        if (pending.length > 0 && enabledIDs.size >= 0) {
            // Only show when bootstrap returned a sources list (empty list still means pending).
            banners.push({
                id: 'pending_enable',
                tone: 'info',
                title: 'Weather received locally - pending platform enable',
                body: 'The bridge can POST weather when the API link is up. Public display requires ops to enable each station source_id on aviationwx.org.',
            });
        }
    }

    return banners;
}

/**
 * firstIncompleteWizardStep picks where Resume should jump after timezone is already set.
 * @param {object} input
 * @returns {WizardStep}
 */
export function firstIncompleteWizardStep({ config = {}, cameras = [], stations = [] } = {}) {
    const api = config.api || {};
    if (!(api.enabled && api.key_set)) {
        return 'api';
    }
    if (!cameras || cameras.length === 0) {
        return 'cameras';
    }
    if (!stations || stations.length === 0) {
        return 'weather';
    }
    return 'done';
}

/**
 * isFirstRunSetup reports whether the welcome setup banner should show.
 * @param {object} [status]
 * @returns {boolean}
 */
export function isFirstRunSetup(status) {
    if (!status) return false;
    if (typeof status.first_run === 'boolean') {
        return status.first_run;
    }
    return (status.total_cameras || 0) === 0 && (status.total_stations || 0) === 0;
}
