from pathlib import Path


def replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{label}: expected exactly one match, found {count}")
    return text.replace(old, new, 1)


settings_page = Path("frontend/src/pages/SettingsPage.vue")
text = settings_page.read_text(encoding="utf-8")

text = replace_once(
    text,
    'const updateProxyPort = ref("");',
    'const updateProxyPort = ref<string | number>("");',
    "proxy port ref type",
)

text = replace_once(
    text,
    '  lastSavedProxySignature = `${updateProxyHost.value.trim()}:${updateProxyPort.value.trim()}`;',
    '  lastSavedProxySignature = `${updateProxyHost.value.trim()}:${normalizeUpdateProxyPortText(updateProxyPort.value)}`;',
    "saved proxy signature normalization",
)

text = replace_once(
    text,
    '''function updateProxyPayload(showErrors = true) {
  const proxyHost = updateProxyHost.value.trim();
  const proxyPortText = updateProxyPort.value.trim();
''',
    '''function normalizeUpdateProxyPortText(value: string | number | null | undefined) {
  return value == null ? "" : String(value).trim();
}

function updateProxyPayload(showErrors = true) {
  const proxyHost = updateProxyHost.value.trim();
  const proxyPortText = normalizeUpdateProxyPortText(updateProxyPort.value);
''',
    "proxy payload port normalization",
)

settings_page.write_text(text, encoding="utf-8")


documentation = Path("docs/UPDATER_PROXY.md")
doc_text = documentation.read_text(encoding="utf-8")
doc_text = replace_once(
    doc_text,
    '“测试代理”会访问 GitHub，成功时显示识别出的协议和延迟；失败时分别显示 HTTP 与 SOCKS5 的错误。测试和检查更新都使用短连接超时，错误代理不会长时间锁住更新页面。',
    '“测试代理”会访问 GitHub，成功时显示识别出的协议和延迟；失败时分别显示 HTTP 与 SOCKS5 的错误。测试和检查更新都使用短连接超时，错误代理不会长时间锁住更新页面。代理端口输入在 WebView2 或浏览器中可能表现为字符串或数字，界面会在保存和测试前统一转换并校验，避免输入类型差异中断按钮动作。',
    "updater proxy documentation",
)
documentation.write_text(doc_text, encoding="utf-8")
