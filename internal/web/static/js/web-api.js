/**
 * Shared web API helpers (testable without DOM).
 */

/** shouldRetryAuth reports whether a 401 response should trigger re-prompt and one retry. */
export function shouldRetryAuth(status, alreadyRetried) {
    return status === 401 && !alreadyRetried;
}
