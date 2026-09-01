from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    target = Path(path)
    text = target.read_text(encoding="utf-8")
    if old not in text:
        raise SystemExit(f"expected block not found in {path}")
    target.write_text(text.replace(old, new, 1), encoding="utf-8")

replace_once(
    "frontend/src/stores/app.ts",
    '''      const proxyLabel = settings.proxyHost && settings.proxyPort\n        ? `更新代理：http://${settings.proxyHost}:${settings.proxyPort}`\n        : "更新代理：直连";\n      useUiStore().toast("更新设置已保存", proxyLabel, "success");''',
    '''      if (settings.proxyHost && settings.proxyPort) {\n        useUiStore().toast(\n          "已使用代理模式",\n          `${settings.proxyHost}:${settings.proxyPort} · 自动识别 HTTP / SOCKS5；可点击“测试代理”确认协议。`,\n          "success",\n        );\n      } else {\n        useUiStore().toast("已恢复直连模式", "软件更新将直接连接 GitHub。", "success");\n      }''',
)

replace_once(
    "frontend/src/pages/SettingsPage.vue",
    '''    ui.toast("代理测试成功", result.message, "success");''',
    '''    ui.toast("已使用代理模式", result.message, "success");''',
)
