<script setup lang="ts">
import { computed, ref, watch } from "vue";
import AppButton from "@/components/ui/AppButton.vue";

interface PromptTemplateItem {
  id: string;
  name: string;
  description: string;
  content: string;
  builtin: boolean;
  updatedAt?: number;
}

interface StoredPromptTemplate {
  id: string;
  name: string;
  content: string;
  updatedAt: number;
}

const props = withDefaults(defineProps<{
  open: boolean;
  title?: string;
  targetLabel?: string;
}>(), {
  title: "提示词模板",
  targetLabel: "提示词",
});

const emit = defineEmits<{
  (event: "close"): void;
  (event: "apply", content: string): void;
}>();

const storageKey = "mcp-devdesk-prompt-templates-v1";
const maxTemplateBytes = 32 * 1024;
const maxCustomTemplates = 20;

const builtinTemplates: PromptTemplateItem[] = [
  {
    id: "continuous-development",
    name: "持续开发 · 自动接班",
    description: "持续执行到完成，并用短摘要让下一次对话快速接上当前进度。",
    builtin: true,
    content: `# 持续开发与自动接班规则

## 开始任务
- 先以用户当前要求为本轮最高目标；能直接继续执行的步骤不要为了汇报进度而中断。
- 如果项目根目录存在 \`PROJECT_STATUS.md\`，在大范围读取代码、文档或历史之前先读取它，用它快速恢复当前进度。
- 然后遵守当前目录实际适用的 \`AGENTS.md\` 以及更具体的项目规则。\`PROJECT_STATUS.md\` 只是进度缓存，不是更高优先级的规则；发生冲突时以用户当前要求和适用的项目规则为准。
- 只读取当前任务真正需要的代码和文档。不要默认遍历整个 docs、全部 Git 历史或重复读取已经明确的长文件。

## 持续执行
- 收到任务后持续推进到当前任务完成，除非遇到必须由用户提供信息、授权或外部条件才能继续的真实阻塞。
- 当前环境允许继续时，不把同一任务拆成“下一轮再做”，也不要只给计划而不执行。
- 用户明确要求只分析、只评估或不要修改时，以用户当前指令为准。

## 自动总结与上下文控制
- 将项目根目录的 \`PROJECT_STATUS.md\` 作为“可覆盖的短摘要”，不要把它写成不断追加的开发日志。
- 在完成一个可独立交付的阶段后、切换大任务前或准备结束本轮工作前，若当前任务允许写文件，则更新一次该摘要。
- 摘要优先控制在 6 KB 内，最多 8 KB；超过时先压缩旧信息再写入，禁止无限增长。
- 摘要只保留：当前目标、已完成结果、关键决策、真实阻塞、下一步、验证结果、关键文件/提交。不要粘贴长代码、完整日志、整段聊天记录或已经失效的历史过程。
- 新会话优先利用这份短摘要恢复进度，不要为了“找回上下文”重新读取整个项目的大量 Markdown。

## 验证与完成
- 修改代码后执行与改动相关的测试、构建或验证；优先使用项目已有命令。
- 最终回复只总结已完成事项、验证结果和真实阻塞项。没有阻塞时，不把本轮能够完成的工作留到下一轮。`,
  },
  {
    id: "strict-engineering",
    name: "严格工程 · 发布闭环",
    description: "适合长期软件项目：根因修复、最小改动、测试构建、状态摘要和发布纪律。",
    builtin: true,
    content: `# 严格工程开发规则

## 上下文读取顺序
- 先确认用户当前目标。
- 若存在 \`PROJECT_STATUS.md\`，先读取它掌握当前进度，再读取适用的 \`AGENTS.md\` / 项目规则，然后只读取本任务相关文件。
- \`PROJECT_STATUS.md\` 是短进度缓存，不覆盖用户当前指令或项目规则。
- 不默认扫描全部文档、全部日志或完整历史；先从摘要和直接相关文件定位问题，需要证据时再扩展读取范围。

## 修改原则
- 优先修复根因，不用吞错、无意义重试、随意延长超时或临时绕过掩盖问题。
- 保持修改范围聚焦，避免顺手重构无关模块；涉及兼容性、数据和安全边界时优先保守处理。
- 不提交密码、Token、Cookie、私钥、真实凭据、本机个人配置或用户运行数据。
- 未经用户明确要求，不创建正式 Release、Tag、生产部署或不可逆外部操作。

## 验证闭环
- 修改前确认现有行为和约束；修改后运行与范围匹配的测试、Lint、构建或 smoke test。
- 测试失败时追查真实原因，不为了“通过”而弱化检查。
- 涉及平台专属行为而当前环境无法实机验证时，明确记录仍需验证的部分。

## 自动状态摘要
- 使用项目根目录 \`PROJECT_STATUS.md\` 保存可接班状态，并采用覆盖式更新，不追加流水账。
- 每个可独立交付阶段完成后更新；优先 <= 6 KB，硬上限 8 KB，超限时压缩旧内容。
- 固定只保留：当前目标、已完成、关键决策、阻塞、下一步、验证、关键文件/提交。
- 删除已经失效的计划、重复描述、长代码和长日志。新会话先读摘要，不重新消费整套历史 Markdown。

## 完成标准
- 能继续执行就继续执行到完成；只有真实阻塞才停下询问。
- 最终说明实际改了什么、验证了什么、还有什么真实风险或阻塞。`,
  },
];

const customTemplates = ref<StoredPromptTemplate[]>([]);
const selectedId = ref(builtinTemplates[0].id);
const editorOpen = ref(false);
const editorId = ref("");
const editorName = ref("");
const editorContent = ref("");
const editorError = ref("");
const pendingDeleteId = ref("");

const templates = computed<PromptTemplateItem[]>(() => [
  ...builtinTemplates,
  ...customTemplates.value.map((template) => ({
    ...template,
    description: "我的自定义模板，可在全局提示词和项目 AGENTS.md 中重复套用。",
    builtin: false,
  })),
]);

const selectedTemplate = computed(() => templates.value.find((template) => template.id === selectedId.value) || templates.value[0]);
const editorBytes = computed(() => new TextEncoder().encode(editorContent.value).length);

function loadCustomTemplates() {
  try {
    const raw = window.localStorage.getItem(storageKey);
    if (!raw) {
      customTemplates.value = [];
      return;
    }
    const parsed = JSON.parse(raw) as unknown;
    if (!Array.isArray(parsed)) throw new Error("invalid template data");
    customTemplates.value = parsed
      .filter((item): item is StoredPromptTemplate => Boolean(
        item
        && typeof item === "object"
        && typeof (item as StoredPromptTemplate).id === "string"
        && typeof (item as StoredPromptTemplate).name === "string"
        && typeof (item as StoredPromptTemplate).content === "string",
      ))
      .slice(0, maxCustomTemplates)
      .map((item) => ({
        id: item.id,
        name: item.name.trim().slice(0, 48),
        content: item.content.trim(),
        updatedAt: Number(item.updatedAt) || Date.now(),
      }))
      .filter((item) => item.name && item.content && new TextEncoder().encode(item.content).length <= maxTemplateBytes);
  } catch {
    customTemplates.value = [];
  }
}

function persistCustomTemplates() {
  window.localStorage.setItem(storageKey, JSON.stringify(customTemplates.value));
}

function newCustomTemplate() {
  editorId.value = "";
  editorName.value = "";
  editorContent.value = "";
  editorError.value = "";
  editorOpen.value = true;
  pendingDeleteId.value = "";
}

function editCustomTemplate(template: PromptTemplateItem) {
  if (template.builtin) return;
  editorId.value = template.id;
  editorName.value = template.name;
  editorContent.value = template.content;
  editorError.value = "";
  editorOpen.value = true;
  pendingDeleteId.value = "";
}

function closeEditor() {
  editorOpen.value = false;
  editorError.value = "";
}

function saveCustomTemplate() {
  const name = editorName.value.trim();
  const content = editorContent.value.trim();
  if (!name) {
    editorError.value = "请填写模板名称。";
    return;
  }
  if (!content) {
    editorError.value = "请填写模板内容。";
    return;
  }
  if (editorBytes.value > maxTemplateBytes) {
    editorError.value = `模板不能超过 ${maxTemplateBytes} bytes。`;
    return;
  }
  if (!editorId.value && customTemplates.value.length >= maxCustomTemplates) {
    editorError.value = `最多保存 ${maxCustomTemplates} 个自定义模板。`;
    return;
  }

  const now = Date.now();
  let id = editorId.value;
  if (id) {
    customTemplates.value = customTemplates.value.map((template) => template.id === id
      ? { ...template, name: name.slice(0, 48), content, updatedAt: now }
      : template);
  } else {
    id = typeof crypto !== "undefined" && typeof crypto.randomUUID === "function"
      ? `custom-${crypto.randomUUID()}`
      : `custom-${now}-${Math.random().toString(16).slice(2)}`;
    customTemplates.value = [
      ...customTemplates.value,
      { id, name: name.slice(0, 48), content, updatedAt: now },
    ];
  }
  persistCustomTemplates();
  selectedId.value = id;
  closeEditor();
}

function requestDelete(template: PromptTemplateItem) {
  if (template.builtin) return;
  if (pendingDeleteId.value !== template.id) {
    pendingDeleteId.value = template.id;
    return;
  }
  customTemplates.value = customTemplates.value.filter((item) => item.id !== template.id);
  persistCustomTemplates();
  if (selectedId.value === template.id) selectedId.value = builtinTemplates[0].id;
  pendingDeleteId.value = "";
}

function applySelectedTemplate() {
  const template = selectedTemplate.value;
  if (!template) return;
  emit("apply", template.content);
  emit("close");
}

watch(() => props.open, (open) => {
  if (!open) return;
  loadCustomTemplates();
  if (!templates.value.some((template) => template.id === selectedId.value)) selectedId.value = builtinTemplates[0].id;
  editorOpen.value = false;
  editorError.value = "";
  pendingDeleteId.value = "";
});
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="prompt-template-backdrop" @click.self="emit('close')">
      <section class="prompt-template-dialog" role="dialog" aria-modal="true" :aria-label="title">
        <header class="prompt-template-header">
          <div>
            <span>Prompt templates</span>
            <h2>{{ title }}</h2>
            <p>选择内置模板，或保存自己的模板后反复一键套用到{{ targetLabel }}。</p>
          </div>
          <AppButton tone="quiet" @click="emit('close')">关闭</AppButton>
        </header>

        <div class="prompt-template-layout">
          <aside class="prompt-template-list">
            <div class="prompt-template-list-heading">
              <strong>模板库</strong>
              <AppButton tone="secondary" compact @click="newCustomTemplate">新建自定义</AppButton>
            </div>
            <button
              v-for="template in templates"
              :key="template.id"
              type="button"
              class="prompt-template-item"
              :class="{ 'is-selected': selectedId === template.id }"
              @click="selectedId = template.id; editorOpen = false; pendingDeleteId = ''"
            >
              <span class="prompt-template-kind">{{ template.builtin ? '内置' : '自定义' }}</span>
              <strong>{{ template.name }}</strong>
              <small>{{ template.description }}</small>
            </button>
          </aside>

          <main class="prompt-template-preview">
            <template v-if="!editorOpen && selectedTemplate">
              <div class="prompt-template-preview-heading">
                <div>
                  <span>{{ selectedTemplate.builtin ? 'Built-in template' : 'Custom template' }}</span>
                  <h3>{{ selectedTemplate.name }}</h3>
                  <p>{{ selectedTemplate.description }}</p>
                </div>
                <div v-if="!selectedTemplate.builtin" class="prompt-template-custom-actions">
                  <AppButton tone="secondary" compact @click="editCustomTemplate(selectedTemplate)">编辑</AppButton>
                  <AppButton tone="quiet" compact @click="requestDelete(selectedTemplate)">
                    {{ pendingDeleteId === selectedTemplate.id ? '再次点击删除' : '删除' }}
                  </AppButton>
                </div>
              </div>
              <textarea class="prompt-template-readonly" :value="selectedTemplate.content" rows="18" readonly spellcheck="false" />
              <div class="prompt-template-footer">
                <small>模板只负责填入编辑框；你仍可继续修改后再保存。内置“自动接班”模板会要求把 PROJECT_STATUS.md 始终压缩为短摘要，避免历史 Markdown 不断膨胀。</small>
                <AppButton tone="primary" @click="applySelectedTemplate">套用此模板</AppButton>
              </div>
            </template>

            <template v-else>
              <div class="prompt-template-preview-heading">
                <div><span>Custom template</span><h3>{{ editorId ? '编辑自定义模板' : '新建自定义模板' }}</h3><p>写入你自己的 AGENTS 规则，以后在全局提示词或任意项目中一键复用。</p></div>
              </div>
              <label class="prompt-template-field">
                <span>模板名称</span>
                <input v-model="editorName" maxlength="48" placeholder="例如：我的长期开发规则" />
              </label>
              <label class="prompt-template-field prompt-template-editor-field">
                <span>模板内容</span>
                <textarea v-model="editorContent" rows="15" spellcheck="false" placeholder="# 我的 Agent 规则&#10;&#10;## 开始任务&#10;- ..." />
                <small>{{ editorBytes }} / {{ maxTemplateBytes }} bytes</small>
              </label>
              <p v-if="editorError" class="prompt-template-error">{{ editorError }}</p>
              <div class="prompt-template-footer">
                <small>自定义模板保存在当前 DevDesk 界面的本地浏览器存储中，不会自动写进项目；只有点击“套用”并保存目标提示词后才会生效。</small>
                <div class="prompt-template-footer-actions">
                  <AppButton tone="quiet" @click="closeEditor">取消</AppButton>
                  <AppButton tone="primary" @click="saveCustomTemplate">保存自定义模板</AppButton>
                </div>
              </div>
            </template>
          </main>
        </div>
      </section>
    </div>
  </Teleport>
</template>

<style scoped>
.prompt-template-backdrop {
  position: fixed;
  inset: 0;
  z-index: 140;
  display: grid;
  place-items: center;
  padding: 18px;
  background: rgba(12, 14, 20, 0.34);
  backdrop-filter: blur(18px) saturate(125%);
}

.prompt-template-dialog {
  display: flex;
  width: min(1040px, calc(100vw - 36px));
  max-height: min(800px, calc(100vh - 36px));
  flex-direction: column;
  overflow: hidden;
  border: 1px solid var(--glass-card-border);
  border-radius: 24px;
  background: color-mix(in srgb, var(--surface-solid) 88%, transparent);
  box-shadow: var(--shadow-float);
  backdrop-filter: blur(34px) saturate(155%);
}

.prompt-template-header,
.prompt-template-preview-heading,
.prompt-template-footer,
.prompt-template-list-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}

.prompt-template-header {
  padding: 24px 26px 20px;
  border-bottom: 1px solid var(--hairline);
}

.prompt-template-header span,
.prompt-template-preview-heading span,
.prompt-template-kind {
  color: var(--text-tertiary);
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.prompt-template-header h2,
.prompt-template-preview-heading h3 {
  margin-top: 4px;
}

.prompt-template-header p,
.prompt-template-preview-heading p {
  margin-top: 5px;
  color: var(--text-secondary);
  font-size: 13px;
}

.prompt-template-layout {
  display: grid;
  min-height: 0;
  grid-template-columns: 310px minmax(0, 1fr);
  flex: 1;
}

.prompt-template-list {
  min-height: 0;
  overflow-y: auto;
  padding: 18px;
  border-right: 1px solid var(--hairline);
  background: color-mix(in srgb, var(--surface-muted) 68%, transparent);
}

.prompt-template-list-heading {
  align-items: center;
  margin-bottom: 12px;
}

.prompt-template-item {
  display: flex;
  width: 100%;
  flex-direction: column;
  align-items: flex-start;
  gap: 5px;
  margin-bottom: 9px;
  padding: 14px;
  border: 1px solid var(--hairline);
  border-radius: 14px;
  background: color-mix(in srgb, var(--surface-solid) 72%, transparent);
  text-align: left;
  cursor: pointer;
  transition: border-color var(--transition-fast), background var(--transition-fast), transform var(--transition-fast);
}

.prompt-template-item:hover {
  transform: translateY(-1px);
  background: var(--surface-hover);
}

.prompt-template-item.is-selected {
  border-color: color-mix(in srgb, var(--accent) 42%, var(--hairline));
  background: var(--accent-soft);
}

.prompt-template-item strong {
  font-size: 14px;
}

.prompt-template-item small {
  color: var(--text-secondary);
  line-height: 1.45;
}

.prompt-template-preview {
  min-width: 0;
  min-height: 0;
  overflow-y: auto;
  padding: 22px 24px 24px;
}

.prompt-template-preview-heading {
  margin-bottom: 16px;
}

.prompt-template-custom-actions,
.prompt-template-footer-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.prompt-template-readonly,
.prompt-template-field input,
.prompt-template-field textarea {
  width: 100%;
  border: 1px solid var(--hairline-strong);
  border-radius: 13px;
  background: color-mix(in srgb, var(--surface-solid) 86%, transparent);
  color: var(--text);
}

.prompt-template-readonly,
.prompt-template-field textarea {
  resize: vertical;
  padding: 15px 16px;
  font-family: var(--font-mono);
  font-size: 12px;
  line-height: 1.6;
}

.prompt-template-readonly {
  min-height: 330px;
}

.prompt-template-field {
  display: grid;
  gap: 7px;
  margin-bottom: 14px;
  color: var(--text-secondary);
  font-size: 12px;
  font-weight: 650;
}

.prompt-template-field input {
  height: 40px;
  padding: 0 12px;
}

.prompt-template-field small,
.prompt-template-footer small {
  color: var(--text-tertiary);
  font-weight: 500;
}

.prompt-template-editor-field textarea {
  min-height: 300px;
}

.prompt-template-footer {
  align-items: center;
  margin-top: 16px;
  padding-top: 16px;
  border-top: 1px solid var(--hairline);
}

.prompt-template-footer > small {
  max-width: 620px;
  line-height: 1.55;
}

.prompt-template-error {
  margin-top: -4px;
  color: var(--danger);
  font-size: 12px;
}

@media (max-width: 760px) {
  .prompt-template-backdrop {
    padding: 8px;
  }

  .prompt-template-dialog {
    width: calc(100vw - 16px);
    max-height: calc(100vh - 16px);
    border-radius: 18px;
  }

  .prompt-template-header {
    padding: 18px;
  }

  .prompt-template-layout {
    display: block;
    overflow-y: auto;
  }

  .prompt-template-list {
    max-height: 250px;
    border-right: 0;
    border-bottom: 1px solid var(--hairline);
  }

  .prompt-template-preview {
    overflow: visible;
    padding: 18px;
  }

  .prompt-template-footer,
  .prompt-template-preview-heading {
    flex-direction: column;
  }
}
</style>
