/**
 * Update banner and apply behavior for Pi supervisor vs IT-managed Docker.
 */

export const DEFAULT_RELEASE_NOTES_URL =
    'https://github.com/alexwitherspoon/aviationwx.org-bridge/releases';

function isSafeHTTPURL(url) {
    try {
        const parsed = new URL(url);
        return parsed.protocol === 'http:' || parsed.protocol === 'https:';
    } catch {
        return false;
    }
}

export function releaseNotesURL(updateInfo) {
    const url = updateInfo?.latest_url;
    if (typeof url === 'string') {
        const trimmed = url.trim();
        if (trimmed !== '' && isSafeHTTPURL(trimmed)) {
            return trimmed;
        }
    }
    return DEFAULT_RELEASE_NOTES_URL;
}

export function canApplyUpdateFromUI(selfUpdateEnabled) {
    return selfUpdateEnabled === true;
}

export function updateBannerLabel(latestVersion, selfUpdateEnabled) {
    const version = latestVersion || '';
    if (canApplyUpdateFromUI(selfUpdateEnabled)) {
        return `Update to ${version}`;
    }
    return `${version} available`;
}

export function manualUpdateConfirmMessage(updateInfo) {
    const latest = updateInfo?.latest_version || 'unknown';
    const url = releaseNotesURL(updateInfo);
    return [
        `Version ${latest} is available.`,
        '',
        'Self-update is disabled in this deployment. Pull and recreate the container with your orchestration tooling.',
        '',
        `Release notes: ${url}`,
    ].join('\n');
}

export function selfUpdateConfirmMessage(updateInfo) {
    const current = updateInfo?.current_version || '';
    const latest = updateInfo?.latest_version || '';
    return [
        `Current version: ${current}`,
        `Latest version: ${latest}`,
        '',
        'This triggers the host supervisor to pull and restart the bridge.',
        'The bridge will be unavailable for 1-2 minutes during the update.',
    ].join('\n');
}
