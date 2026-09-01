from pathlib import Path

path = Path("frontend/src/pages/SettingsPage.vue")
text = path.read_text(encoding="utf-8")
replacements = [
    (
        '''<button type="button" class="app-button is-quiet" :disabled="updateSettingsSaving" @pointerdown="markUpdateAction('保存更新设置')" @click="saveUpdatePreferences(false)">''',
        '''<button type="button" class="app-button is-quiet" :disabled="updateSettingsSaving" @pointerdown.prevent.stop="saveUpdatePreferences(false)" @click="saveUpdatePreferences(false)">''',
    ),
    (
        '''<button type="button" class="app-button is-secondary" :disabled="updateProxyTesting" @pointerdown="markUpdateAction('测试代理')" @click="testUpdateProxy">''',
        '''<button type="button" class="app-button is-secondary" :disabled="updateProxyTesting" @pointerdown.prevent.stop="testUpdateProxy" @click="testUpdateProxy">''',
    ),
    (
        '''<button type="button" class="app-button is-secondary" :disabled="updateChecking" @pointerdown="markUpdateAction('检查更新')" @click="checkForUpdate">''',
        '''<button type="button" class="app-button is-secondary" :disabled="updateChecking" @pointerdown.prevent.stop="checkForUpdate" @click="checkForUpdate">''',
    ),
    (
        '''<AppButton v-if="app.updateRelease?.updateAvailable" tone="primary" icon="play" :loading="app.actionPending === 'install-update'" @click="installAvailableUpdate">立即更新到 {{ app.updateRelease.latestVersion }}</AppButton>''',
        '''<button v-if="app.updateRelease?.updateAvailable" type="button" class="app-button is-primary" :disabled="app.actionPending === 'install-update'" @pointerdown.prevent.stop="installAvailableUpdate" @click="installAvailableUpdate">
            <span v-if="app.actionPending === 'install-update'" class="button-spinner" /><AppIcon v-else name="play" :size="16" /><span>立即更新到 {{ app.updateRelease.latestVersion }}</span>
          </button>''',
    ),
]
for old, new in replacements:
    if old not in text:
        raise SystemExit(f"expected updater button markup not found: {old[:100]}")
    text = text.replace(old, new, 1)
path.write_text(text, encoding="utf-8")
Path("tools/apply_updater_pointer_action_fix.py").unlink()
