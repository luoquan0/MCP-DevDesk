from pathlib import Path


def replace_once(text: str, old: str, new: str, label: str) -> str:
    count = text.count(old)
    if count != 1:
        raise SystemExit(f"{label}: expected exactly one match, found {count}")
    return text.replace(old, new, 1)


workspace_path = Path("frontend/src/pages/WorkspaceHubPage.vue")
workspace = workspace_path.read_text(encoding="utf-8")

workspace = replace_once(
    workspace,
    'async function runProject(project: Project, action: "start" | "stop" | "restart") {',
    '''async function repairProjectTunnelDNS(project: Project | null) {\n  if (!project) return;\n  const instance = instanceForProject(project);\n  if (!instance?.domain || !instance.tunnelId) {\n    openRuntimeConfig(project);\n    ui.toast("暂时无法修复 DNS", "请先保存公网域名并成功创建或复用 Tunnel。", "info");\n    return;\n  }\n  try {\n    await app.repairInstanceTunnelDNS(instance.id);\n  } catch (error) {\n    ui.toast("修复 DNS 失败", error instanceof Error ? error.message : String(error), "danger");\n  }\n}\n\nasync function runProject(project: Project, action: "start" | "stop" | "restart") {''',
    "insert workspace DNS repair action",
)

workspace = replace_once(
    workspace,
    '            <AppButton tone="secondary" compact icon="settings" @click="openRuntimeConfig(project)">配置</AppButton>\n',
    '            <AppButton tone="secondary" compact icon="settings" @click="openRuntimeConfig(project)">配置</AppButton>\n            <AppButton v-if="instanceForProject(project)?.domain && instanceForProject(project)?.tunnelId" tone="quiet" compact icon="refresh" :loading="app.actionPending === `repair-instance-dns-${instanceForProject(project)?.id}`" @click="repairProjectTunnelDNS(project)">修复DNS</AppButton>\n',
    "add project-row DNS repair button",
)

workspace = replace_once(
    workspace,
    '            <ToggleSwitch v-model="runtimeForm.reuseTunnel" label="复用已有 Tunnel" />\n            <small v-if="instanceForProject(runtimeProject)?.remoteMcpUrl" class="workspace-tunnel-url">{{ instanceForProject(runtimeProject)?.remoteMcpUrl }}</small>\n',
    '''            <ToggleSwitch v-model="runtimeForm.reuseTunnel" label="复用已有 Tunnel" />\n            <small v-if="instanceForProject(runtimeProject)?.remoteMcpUrl" class="workspace-tunnel-url">{{ instanceForProject(runtimeProject)?.remoteMcpUrl }}</small>\n            <div v-if="instanceForProject(runtimeProject)?.domain && instanceForProject(runtimeProject)?.tunnelId" class="form-footer">\n              <small>Tunnel 正常但域名没有 DNS 时，可重新绑定当前 Tunnel UUID 并验证公网解析。</small>\n              <AppButton tone="secondary" icon="refresh" :loading="app.actionPending === `repair-instance-dns-${instanceForProject(runtimeProject)?.id}`" @click="repairProjectTunnelDNS(runtimeProject)">修复 DNS</AppButton>\n            </div>\n''',
    "add runtime-dialog DNS repair button",
)
workspace_path.write_text(workspace, encoding="utf-8")

instances_path = Path("frontend/src/pages/InstancesPage.vue")
instances = instances_path.read_text(encoding="utf-8")
instances = replace_once(
    instances,
    '''async function repairTunnelDNS(instance: MCPInstance) {\n  try {\n    await app.repairInstanceTunnelDNS(instance.id);\n  } catch (error) {\n    ui.toast("修复 DNS 失败", errorMessage(error), "danger");\n  }\n}\n\n''',
    "",
    "remove legacy repair handler",
)
instances = replace_once(
    instances,
    '          <AppButton v-if="instance.domain && instance.tunnelId" tone="secondary" icon="refresh" :loading="app.actionPending === `repair-instance-dns-${instance.id}`" @click="repairTunnelDNS(instance)">修复 DNS</AppButton>\n',
    "",
    "remove legacy repair button",
)
instances_path.write_text(instances, encoding="utf-8")

Path(__file__).unlink()
print("Workspace DNS repair UI patch applied.")
