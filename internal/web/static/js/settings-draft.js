/**
 * Pure helpers for settings draft / unsaved state. Used by the web console and unit tests.
 */

/**
 * When true, do not hydrate settings form controls from GET /api/config (user has local edits).
 * @param {{ timezoneDirty: boolean, webConsoleDirty: boolean, uploadSettingsDirty: boolean, apiLinkDirty?: boolean }} s
 * @returns {boolean}
 */
export function shouldSkipSettingsHydrate(s) {
    return Boolean(s.timezoneDirty || s.webConsoleDirty || s.uploadSettingsDirty || s.apiLinkDirty);
}

/**
 * When true, the browser may show a leave confirmation (beforeunload).
 * @param {{ timezoneDirty: boolean, webConsoleDirty: boolean, uploadSettingsDirty: boolean, apiLinkDirty?: boolean, cameraFormDirty: boolean }} s
 * @returns {boolean}
 */
export function shouldWarnBeforePageLeave(s) {
    return Boolean(
        s.timezoneDirty || s.webConsoleDirty || s.uploadSettingsDirty || s.apiLinkDirty || s.cameraFormDirty
    );
}
