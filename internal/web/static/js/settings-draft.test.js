import assert from 'node:assert/strict';
import test from 'node:test';
import { shouldSkipSettingsHydrate, shouldWarnBeforePageLeave } from './settings-draft.js';

test('shouldSkipSettingsHydrate is false when no section is dirty', () => {
    assert.equal(
        shouldSkipSettingsHydrate({ timezoneDirty: false, webConsoleDirty: false, uploadSettingsDirty: false }),
        false
    );
});

test('shouldSkipSettingsHydrate is true when timezone is dirty', () => {
    assert.equal(
        shouldSkipSettingsHydrate({ timezoneDirty: true, webConsoleDirty: false, uploadSettingsDirty: false }),
        true
    );
});

test('shouldSkipSettingsHydrate is true when web console is dirty', () => {
    assert.equal(
        shouldSkipSettingsHydrate({ timezoneDirty: false, webConsoleDirty: true, uploadSettingsDirty: false }),
        true
    );
});

test('shouldSkipSettingsHydrate is true when upload settings are dirty', () => {
    assert.equal(
        shouldSkipSettingsHydrate({ timezoneDirty: false, webConsoleDirty: false, uploadSettingsDirty: true }),
        true
    );
});

test('shouldWarnBeforePageLeave is false when nothing is dirty', () => {
    assert.equal(
        shouldWarnBeforePageLeave({
            timezoneDirty: false,
            webConsoleDirty: false,
            uploadSettingsDirty: false,
            cameraFormDirty: false,
        }),
        false
    );
});

test('shouldWarnBeforePageLeave is true when camera form is dirty', () => {
    assert.equal(
        shouldWarnBeforePageLeave({
            timezoneDirty: false,
            webConsoleDirty: false,
            uploadSettingsDirty: false,
            cameraFormDirty: true,
        }),
        true
    );
});

test('shouldWarnBeforePageLeave is true when any settings flag is dirty', () => {
    assert.equal(
        shouldWarnBeforePageLeave({
            timezoneDirty: true,
            webConsoleDirty: false,
            uploadSettingsDirty: false,
            cameraFormDirty: false,
        }),
        true
    );
});
