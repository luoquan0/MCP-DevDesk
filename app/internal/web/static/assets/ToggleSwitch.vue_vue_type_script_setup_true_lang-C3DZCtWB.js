import{d as M,l as H,h as Q,c as u,m as W,e as t,t as a,b as f,_ as T,w as y,F as N,r as X,p as w,x as I,y as R,T as Y,q as v,k as P,o as i,i as g,n as q,E as Z}from"./index-DcFhNq8Q.js";const ee=["aria-label"],te={class:"prompt-template-header"},le={class:"prompt-template-layout"},oe={class:"prompt-template-list"},ne={class:"prompt-template-list-heading"},ae=["onClick"],se={class:"prompt-template-kind"},ie={class:"prompt-template-preview"},ue={class:"prompt-template-preview-heading"},de={key:0,class:"prompt-template-custom-actions"},re=["value"],pe={class:"prompt-template-footer"},me={class:"prompt-template-preview-heading"},ce={class:"prompt-template-field"},ve={class:"prompt-template-field prompt-template-editor-field"},fe={key:0,class:"prompt-template-error"},Te={class:"prompt-template-footer"},ye={class:"prompt-template-footer-actions"},_="mcp-devdesk-prompt-templates-v1",x=32*1024,V=20,ge=M({__name:"PromptTemplateDialog",props:{open:{type:Boolean},title:{default:"提示词模板"},targetLabel:{default:"提示词"}},emits:["close","apply"],setup(n,{emit:B}){const U=n,S=B,p=[{id:"continuous-development",name:"持续开发 · 自动接班",description:"持续执行到完成，并用短摘要让下一次对话快速接上当前进度。",builtin:!0,content:`# 持续开发与自动接班规则

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
- 最终回复只总结已完成事项、验证结果和真实阻塞项。没有阻塞时，不把本轮能够完成的工作留到下一轮。`},{id:"strict-engineering",name:"严格工程 · 发布闭环",description:"适合长期软件项目：根因修复、最小改动、测试构建、状态摘要和发布纪律。",builtin:!0,content:`# 严格工程开发规则

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
- 最终说明实际改了什么、验证了什么、还有什么真实风险或阻塞。`}],s=v([]),m=v(p[0].id),k=v(!1),A=v(""),E=v(""),b=v(""),d=v(""),c=v(""),$=P(()=>[...p,...s.value.map(o=>({...o,description:"我的自定义模板，可在全局提示词和项目 AGENTS.md 中重复套用。",builtin:!1}))]),r=P(()=>$.value.find(o=>o.id===m.value)||$.value[0]),O=P(()=>new TextEncoder().encode(b.value).length);function G(){try{const o=window.localStorage.getItem(_);if(!o){s.value=[];return}const e=JSON.parse(o);if(!Array.isArray(e))throw new Error("invalid template data");s.value=e.filter(l=>!!(l&&typeof l=="object"&&typeof l.id=="string"&&typeof l.name=="string"&&typeof l.content=="string")).slice(0,V).map(l=>({id:l.id,name:l.name.trim().slice(0,48),content:l.content.trim(),updatedAt:Number(l.updatedAt)||Date.now()})).filter(l=>l.name&&l.content&&new TextEncoder().encode(l.content).length<=x)}catch{s.value=[]}}function h(){window.localStorage.setItem(_,JSON.stringify(s.value))}function K(){A.value="",E.value="",b.value="",d.value="",k.value=!0,c.value=""}function L(o){o.builtin||(A.value=o.id,E.value=o.name,b.value=o.content,d.value="",k.value=!0,c.value="")}function J(){k.value=!1,d.value=""}function F(){const o=E.value.trim(),e=b.value.trim();if(!o){d.value="请填写模板名称。";return}if(!e){d.value="请填写模板内容。";return}if(O.value>x){d.value=`模板不能超过 ${x} bytes。`;return}if(!A.value&&s.value.length>=V){d.value=`最多保存 ${V} 个自定义模板。`;return}const l=Date.now();let C=A.value;C?s.value=s.value.map(D=>D.id===C?{...D,name:o.slice(0,48),content:e,updatedAt:l}:D):(C=typeof crypto<"u"&&typeof crypto.randomUUID=="function"?`custom-${crypto.randomUUID()}`:`custom-${l}-${Math.random().toString(16).slice(2)}`,s.value=[...s.value,{id:C,name:o.slice(0,48),content:e,updatedAt:l}]),h(),m.value=C,J()}function j(o){if(!o.builtin){if(c.value!==o.id){c.value=o.id;return}s.value=s.value.filter(e=>e.id!==o.id),h(),m.value===o.id&&(m.value=p[0].id),c.value=""}}function z(){const o=r.value;o&&(S("apply",o.content),S("close"))}return H(()=>U.open,o=>{o&&(G(),$.value.some(e=>e.id===m.value)||(m.value=p[0].id),k.value=!1,d.value="",c.value="")}),(o,e)=>(i(),Q(Y,{to:"body"},[n.open?(i(),u("div",{key:0,class:"prompt-template-backdrop",onClick:e[5]||(e[5]=W(l=>S("close"),["self"]))},[t("section",{class:"prompt-template-dialog",role:"dialog","aria-modal":"true","aria-label":n.title},[t("header",te,[t("div",null,[e[6]||(e[6]=t("span",null,"Prompt templates",-1)),t("h2",null,a(n.title),1),t("p",null,"选择内置模板，或保存自己的模板后反复一键套用到"+a(n.targetLabel)+"。",1)]),f(T,{tone:"quiet",onClick:e[0]||(e[0]=l=>S("close"))},{default:y(()=>[...e[7]||(e[7]=[g("关闭",-1)])]),_:1})]),t("div",le,[t("aside",oe,[t("div",ne,[e[9]||(e[9]=t("strong",null,"模板库",-1)),f(T,{tone:"secondary",compact:"",onClick:K},{default:y(()=>[...e[8]||(e[8]=[g("新建自定义",-1)])]),_:1})]),(i(!0),u(N,null,X($.value,l=>(i(),u("button",{key:l.id,type:"button",class:q(["prompt-template-item",{"is-selected":m.value===l.id}]),onClick:C=>{m.value=l.id,k.value=!1,c.value=""}},[t("span",se,a(l.builtin?"内置":"自定义"),1),t("strong",null,a(l.name),1),t("small",null,a(l.description),1)],10,ae))),128))]),t("main",ie,[!k.value&&r.value?(i(),u(N,{key:0},[t("div",ue,[t("div",null,[t("span",null,a(r.value.builtin?"Built-in template":"Custom template"),1),t("h3",null,a(r.value.name),1),t("p",null,a(r.value.description),1)]),r.value.builtin?w("",!0):(i(),u("div",de,[f(T,{tone:"secondary",compact:"",onClick:e[1]||(e[1]=l=>L(r.value))},{default:y(()=>[...e[10]||(e[10]=[g("编辑",-1)])]),_:1}),f(T,{tone:"quiet",compact:"",onClick:e[2]||(e[2]=l=>j(r.value))},{default:y(()=>[g(a(c.value===r.value.id?"再次点击删除":"删除"),1)]),_:1})]))]),t("textarea",{class:"prompt-template-readonly",value:r.value.content,rows:"18",readonly:"",spellcheck:"false"},null,8,re),t("div",pe,[e[12]||(e[12]=t("small",null,"模板只负责填入编辑框；你仍可继续修改后再保存。内置“自动接班”模板会要求把 PROJECT_STATUS.md 始终压缩为短摘要，避免历史 Markdown 不断膨胀。",-1)),f(T,{tone:"primary",onClick:z},{default:y(()=>[...e[11]||(e[11]=[g("套用此模板",-1)])]),_:1})])],64)):(i(),u(N,{key:1},[t("div",me,[t("div",null,[e[13]||(e[13]=t("span",null,"Custom template",-1)),t("h3",null,a(A.value?"编辑自定义模板":"新建自定义模板"),1),e[14]||(e[14]=t("p",null,"写入你自己的 AGENTS 规则，以后在全局提示词或任意项目中一键复用。",-1))])]),t("label",ce,[e[15]||(e[15]=t("span",null,"模板名称",-1)),I(t("input",{"onUpdate:modelValue":e[3]||(e[3]=l=>E.value=l),maxlength:"48",placeholder:"例如：我的长期开发规则"},null,512),[[R,E.value]])]),t("label",ve,[e[16]||(e[16]=t("span",null,"模板内容",-1)),I(t("textarea",{"onUpdate:modelValue":e[4]||(e[4]=l=>b.value=l),rows:"15",spellcheck:"false",placeholder:`# 我的 Agent 规则

## 开始任务
- ...`},null,512),[[R,b.value]]),t("small",null,a(O.value)+" / "+a(x)+" bytes",1)]),d.value?(i(),u("p",fe,a(d.value),1)):w("",!0),t("div",Te,[e[19]||(e[19]=t("small",null,"自定义模板保存在当前 DevDesk 界面的本地浏览器存储中，不会自动写进项目；只有点击“套用”并保存目标提示词后才会生效。",-1)),t("div",ye,[f(T,{tone:"quiet",onClick:J},{default:y(()=>[...e[17]||(e[17]=[g("取消",-1)])]),_:1}),f(T,{tone:"primary",onClick:F},{default:y(()=>[...e[18]||(e[18]=[g("保存自定义模板",-1)])]),_:1})])])],64))])])],8,ee)])):w("",!0)]))}}),Ae=Z(ge,[["__scopeId","data-v-4999ca90"]]),ke={key:0,class:"toggle-copy"},be={key:0},Ce={key:1},we=["aria-checked","disabled"],Ee=M({__name:"ToggleSwitch",props:{modelValue:{type:Boolean},disabled:{type:Boolean},label:{},description:{}},emits:["update:modelValue"],setup(n,{emit:B}){const U=B;return(S,p)=>(i(),u("label",{class:q(["toggle-row",{"is-disabled":n.disabled}])},[n.label||n.description?(i(),u("span",ke,[n.label?(i(),u("strong",be,a(n.label),1)):w("",!0),n.description?(i(),u("small",Ce,a(n.description),1)):w("",!0)])):w("",!0),t("button",{class:"toggle-switch",type:"button",role:"switch","aria-checked":n.modelValue,disabled:n.disabled,onClick:p[0]||(p[0]=s=>U("update:modelValue",!n.modelValue))},[...p[1]||(p[1]=[t("span",null,null,-1)])],8,we)],2))}});export{Ae as P,Ee as _};
