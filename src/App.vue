<template>
  <main v-if="isHTMLPreviewRoute" class="html-viewer-shell">
    <div v-if="htmlViewerError" class="html-viewer-state">
      <FileDown :size="30" />
      <h1>HTML 预览不可用</h1>
      <p>{{ htmlViewerError }}</p>
      <a class="command" :href="htmlViewerBackURL">返回文档</a>
    </div>
    <iframe
      v-else-if="htmlViewerContent && htmlViewerSrcdoc"
      class="html-viewer-frame"
      sandbox="allow-scripts"
      :srcdoc="htmlViewerSrcdoc"
      :title="htmlViewerContent.title || htmlViewerContent.file_path"
    ></iframe>
    <div v-else class="html-viewer-state">
      <BookOpen :size="30" />
      <p>正在加载 HTML 预览</p>
    </div>
  </main>
  <main v-else class="app-shell" :class="{ 'ai-mode': tab === 'ai' }">
    <aside class="sidebar">
      <div class="brand">
        <div class="brand-mark" aria-hidden="true">
          <img src="/icon.svg" alt="" />
        </div>
        <div>
          <h1>DocHarbor</h1>
          <p>Git 文档入口</p>
        </div>
      </div>

      <button class="primary-action" type="button" @click="showRepoForm = true">
        <Plus :size="16" />
        新增仓库
      </button>

      <nav class="global-nav" aria-label="全局功能">
        <button :class="{ active: tab === 'ai' }" type="button" @click="switchTab('ai')">
          <Bot :size="16" />
          AI 问答
        </button>
        <button :class="{ active: tab === 'ai-config' }" type="button" @click="switchTab('ai-config')">
          <SlidersHorizontal :size="16" />
          AI 配置
        </button>
        <button :class="{ active: tab === 'ai-diagnostics' }" type="button" @click="switchTab('ai-diagnostics')">
          <Activity :size="16" />
          AI 诊断
        </button>
        <button :class="{ active: tab === 'settings' }" type="button" @click="switchTab('settings')">
          <Settings :size="16" />
          系统设置
        </button>
      </nav>

      <div class="repo-list">
        <button
          v-for="repo in repos"
          :key="repo.id"
          class="repo-item"
          :class="{ active: !isGlobalTab && selectedRepo?.id === repo.id }"
          type="button"
          @click="openRepo(repo)"
        >
          <span class="repo-name">{{ repo.name }}</span>
          <span class="repo-meta">{{ repo.default_branch }} · {{ repo.latest_scan?.status || '未扫描' }}</span>
        </button>
      </div>
    </aside>

    <section class="workspace">
      <header class="topbar">
        <div class="topbar-summary">
          <h2>{{ workspaceTitle }}</h2>
          <p v-if="workspaceSubtitle">{{ workspaceSubtitle }}</p>
          <div v-if="!isGlobalTab && githubWebhookURL" class="webhook-url">
            <span>GitHub Webhook</span>
            <code>{{ githubWebhookURL }}</code>
            <button class="mini-command" type="button" :disabled="webhookSecretLoading" @click="toggleWebhookSecret">
              {{ webhookSecretVisible ? '隐藏 Secret' : '显示 Secret' }}
            </button>
            <button v-if="webhookSecretVisible && webhookSecret" class="mini-command" type="button" @click="copyWebhookSecret">
              复制 Secret
            </button>
            <code v-if="webhookSecretVisible && webhookSecret" class="secret-value">{{ webhookSecret }}</code>
            <span v-if="webhookSecretVisible && !webhookSecret" class="secret-missing">未配置 GITHUB_WEBHOOK_SECRET</span>
          </div>
          <p v-if="!selectedRepo && !isGlobalTab">配置 Git 仓库后开始同步和扫描文档</p>
        </div>
        <div class="topbar-actions">
          <button v-if="selectedRepo && !isGlobalTab" class="icon-button" type="button" title="刷新列表" @click="loadAll">
            <RefreshCw :size="16" />
          </button>
          <button v-if="selectedRepo && !isGlobalTab" class="command" type="button" :disabled="busy" @click="scanSelected">
            <RefreshCw :size="16" />
            扫描
          </button>
          <button v-if="selectedRepo && !isGlobalTab" class="command" type="button" @click="editSelected">
            <Settings :size="16" />
            配置
          </button>
        </div>
      </header>

      <div v-if="error" class="alert">{{ error }}</div>

      <div v-if="showRepoForm" class="modal-backdrop" @click.self="showRepoForm = false">
        <form class="modal" @submit.prevent="saveRepo">
          <div class="modal-header">
            <h3>{{ editingRepo?.id ? '编辑仓库' : '新增仓库' }}</h3>
            <button class="icon-button" type="button" title="关闭" @click="showRepoForm = false">
              <X :size="16" />
            </button>
          </div>
          <label>
            名称
            <input v-model="form.name" required />
          </label>
          <label>
            Slug
            <input v-model="form.slug" placeholder="留空按名称生成" />
          </label>
          <label>
            Git URL
            <input v-model="form.repo_url" required placeholder="git@github.com:org/repo.git" />
          </label>
          <div class="form-grid">
            <label>
              默认分支
              <input v-model="form.default_branch" placeholder="main" />
            </label>
            <label>
              扫描周期秒
              <input v-model.number="form.sync_interval_seconds" type="number" min="60" />
            </label>
          </div>
          <label>
            追踪分支
            <input v-model="trackedBranchesText" placeholder="*, main, release/*" />
          </label>
          <label>
            智能最新 include
            <input v-model="latestIncludeText" placeholder="*" />
          </label>
          <label>
            智能最新 exclude
            <input v-model="latestExcludeText" placeholder="archive/*, tmp/*, dependabot/*" />
          </label>
          <label>
            分支优先级
            <input v-model="branchPriorityText" placeholder="main, master, release/*, develop" />
          </label>
          <label>
            扫描目录
            <textarea v-model="scanPathsText" rows="3" placeholder="doc&#10;docs"></textarea>
          </label>
          <div class="modal-actions">
            <button class="command" type="button" @click="showRepoForm = false">取消</button>
            <button class="primary-action" type="submit" :disabled="busy">
              <Save :size="16" />
              保存
            </button>
          </div>
        </form>
      </div>

      <section v-if="!selectedRepo && !isGlobalTab" class="empty-state">
        <FileText :size="34" />
        <h3>暂无仓库</h3>
        <p>新增仓库后，DocHarbor 会 clone mirror、扫描配置目录，并生成智能最新文档入口。</p>
      </section>

      <section v-if="selectedRepo || isGlobalTab" class="work-grid">
        <nav v-if="!isGlobalTab" class="tabs">
          <button :class="{ active: tab === 'docs' }" type="button" @click="switchTab('docs')">
            <BookOpen :size="16" />
            文档
          </button>
          <button :class="{ active: tab === 'history' }" type="button" @click="switchTab('history')">
            <GitBranch :size="16" />
            历史
          </button>
          <button :class="{ active: tab === 'runs' }" type="button" @click="switchTab('runs')">
            <ListChecks :size="16" />
            扫描
          </button>
        </nav>

        <section v-if="tab === 'docs' && selectedRepo" class="docs-panel">
          <div class="browser-toolbar">
            <div class="segmented">
              <button :class="{ active: viewMode === 'latest' }" type="button" @click="switchView('latest')">智能最新</button>
              <button :class="{ active: viewMode === 'branch' }" type="button" @click="switchView('branch')">按分支</button>
            </div>
            <select v-model="selectedBranch" :disabled="viewMode !== 'branch'" @change="loadBranchFiles">
              <option v-for="branch in branches" :key="branch.ref_name" :value="branch.ref_name">{{ branch.ref_name }}</option>
            </select>
            <div class="search-box">
              <Search :size="15" />
              <input v-model="searchText" placeholder="搜索标题或路径" @keydown.enter="runSearch" />
            </div>
          </div>

          <div class="doc-layout">
            <aside class="file-pane">
              <div class="crumbs">
                <button class="parent-dir" type="button" :disabled="!canGoParent" title="返回上一级" @click="goParent">
                  <ChevronLeft :size="15" />
                  上一级
                </button>
                <nav class="breadcrumb-trail" aria-label="目录路径">
                  <button type="button" @click="loadFiles('.')">根目录</button>
                  <template v-for="crumb in breadcrumbs" :key="crumb.path">
                    <span>/</span>
                    <button type="button" @click="loadFiles(crumb.path)">{{ crumb.label }}</button>
                  </template>
                  <template v-if="currentDir === searchResultDir">
                    <span>/</span>
                    <span>搜索结果</span>
                  </template>
                </nav>
              </div>
              <div class="file-list">
                <button
                  v-for="entry in files"
                  :key="`${entry.kind}:${entry.path}:${entry.version_id || 0}`"
                  class="file-row"
                  :class="{ active: selectedFile?.version_id === entry.version_id }"
                  type="button"
                  @click="entry.kind === 'dir' ? loadFiles(entry.path) : openFile(entry)"
                >
                  <Folder v-if="entry.kind === 'dir'" :size="16" />
                  <FileText v-else :size="16" />
                  <span>{{ entry.kind === 'dir' ? entry.name : entry.title || entry.name }}</span>
                  <small v-if="entry.kind === 'file'">{{ entry.source_branch }}</small>
                </button>
              </div>
            </aside>

            <article class="preview-pane">
              <div v-if="!fileContent" class="empty-preview">
                <BookOpen :size="30" />
                <p>选择左侧文件查看预览和版本状态</p>
              </div>
              <template v-else>
                <header class="file-header">
                  <div>
                    <h3>{{ fileContent.title || fileContent.file_path }}</h3>
                    <p>{{ fileContent.file_path }}</p>
                    <div class="meta-line">
                      <span>{{ fileContent.branch }}</span>
                      <span>{{ shortSha(fileContent.source_commit_sha) }}</span>
                      <span>{{ formatTime(fileContent.last_commit_time) }}</span>
                      <span>{{ formatBytes(fileContent.file_size) }}</span>
                    </div>
                  </div>
                  <div class="file-actions">
                    <a class="command" :href="downloadURL(selectedRepo.id, fileContent.version_id)">
                      <Download :size="16" />
                      下载
                    </a>
                    <a
                      v-if="fileContent.previewable && isHTMLContent(fileContent)"
                      class="command"
                      :href="htmlPreviewURL(selectedRepo.id, fileContent.version_id)"
                      target="_blank"
                      rel="noreferrer"
                    >
                      <FileText :size="16" />
                      独立打开
                    </a>
                    <button class="command" type="button" @click="loadDocHistory(fileContent.document_id)">
                      <History :size="16" />
                      文件历史
                    </button>
                    <button class="command" type="button" :disabled="!canAskWithCurrentFile" @click="askWithCurrentFile">
                      <Bot :size="16" />
                      基于本文提问
                    </button>
                  </div>
                </header>

                <iframe
                  v-if="fileContent.previewable && isHTMLContent(fileContent) && htmlPreviewSrcdoc"
                  class="html-preview-frame"
                  sandbox="allow-scripts"
                  :srcdoc="htmlPreviewSrcdoc"
                  :title="fileContent.title || fileContent.file_path"
                ></iframe>
                <div v-else-if="fileContent.previewable && renderedHtml" class="markdown-body" v-html="renderedHtml"></div>
                <div v-else class="download-only">
                  <FileDown :size="30" />
                  <h4>当前文件不支持预览</h4>
                  <p>支持 Markdown 和 HTML 预览，其他文件可单文件下载。</p>
                </div>

                <section class="detail-split">
                  <div>
                    <h4>分支状态</h4>
                    <div class="compact-table">
                      <div v-for="version in fileContent.versions || []" :key="version.id" class="table-row">
                        <span>{{ version.branch }}</span>
                        <span class="status" :class="version.status">{{ version.status }}</span>
                        <span>{{ version.file_path }}</span>
                      </div>
                    </div>
                  </div>
                  <div>
                    <h4>路径事件</h4>
                    <div class="compact-table">
                      <div v-for="event in docEvents" :key="event.id" class="table-row">
                        <span>{{ event.event_type }}</span>
                        <span>{{ event.branch }}</span>
                        <span>{{ event.old_path || event.new_path }}</span>
                      </div>
                      <p v-if="!docEvents.length" class="muted">暂无删除、移动或重命名事件。</p>
                    </div>
                  </div>
                </section>
              </template>
            </article>
          </div>
        </section>

        <section v-if="tab === 'history' && selectedRepo" class="history-panel">
          <div class="browser-toolbar">
            <select v-model="historyBranch" @change="changeHistoryBranch">
              <option value="">全部分支</option>
              <option v-for="branch in branches" :key="branch.ref_name" :value="branch.ref_name">{{ branch.ref_name }}</option>
            </select>
            <button class="command" type="button" @click="loadHistory">
              <RefreshCw :size="16" />
              刷新
            </button>
          </div>
          <div class="history-layout">
            <div class="git-graph-scroll">
              <div class="git-graph-table" :style="{ '--graph-width': `${graphWidth}px` }">
                <div class="git-graph-head">
                  <span>Graph</span>
                  <span>Description</span>
                  <span>Date</span>
                  <span>Author</span>
                  <span>Commit</span>
                </div>
                <div v-if="!graphRows.length" class="graph-empty">
                  暂无提交历史
                </div>
                <button
                  v-for="row in graphRows"
                  :key="row.commit.sha"
                  class="git-graph-row"
                  :class="{ active: selectedCommit?.sha === row.commit.sha }"
                  type="button"
                  @click="openCommit(row.commit.sha)"
                >
                  <span class="git-graph-cell" :title="row.commit.parents.length ? `parents: ${row.commit.parents.join(', ')}` : 'root commit'">
                    <svg :width="graphWidth" :height="graphRowHeight" :viewBox="`0 0 ${graphWidth} ${graphRowHeight}`" aria-hidden="true">
                      <line
                        v-for="lane in row.beforeLanes"
                        :key="`before-${lane}`"
                        :x1="graphLaneX(lane)"
                        :x2="graphLaneX(lane)"
                        y1="0"
                        :y2="graphCenterY"
                        :stroke="graphLineColor(lane)"
                        stroke-width="3"
                        stroke-linecap="round"
                      />
                      <line
                        v-for="lane in row.afterLanes"
                        :key="`after-${lane}`"
                        :x1="graphLaneX(lane)"
                        :x2="graphLaneX(lane)"
                        :y1="graphCenterY"
                        :y2="graphRowHeight"
                        :stroke="graphLineColor(lane)"
                        stroke-width="3"
                        stroke-linecap="round"
                      />
                      <path
                        v-for="lane in row.connectorLanes"
                        :key="`connector-${lane}`"
                        :d="graphConnectorPath(row.lane, lane)"
                        :stroke="graphLineColor(lane)"
                        stroke-width="3"
                        stroke-linecap="round"
                        fill="none"
                      />
                      <circle :cx="graphLaneX(row.lane)" :cy="graphCenterY" r="6" :fill="graphLineColor(row.lane)" />
                      <circle :cx="graphLaneX(row.lane)" :cy="graphCenterY" r="4" fill="#ffffff" />
                      <circle :cx="graphLaneX(row.lane)" :cy="graphCenterY" r="2.5" :fill="graphLineColor(row.lane)" />
                    </svg>
                  </span>
                  <span class="git-desc-cell" :title="row.commit.decorations ? `${row.commit.decorations} ${row.commit.message}` : row.commit.message">
                    <span v-if="row.badges.length" class="decoration-list">
                      <span v-for="badge in row.badges" :key="`${row.commit.sha}:${badge.label}`" class="ref-badge" :class="badge.type">
                        {{ badge.label }}
                      </span>
                    </span>
                    <span class="commit-message" :class="{ merge: row.isMerge }">{{ row.commit.message }}</span>
                  </span>
                  <span class="git-date-cell">{{ formatHistoryTime(row.commit.commit_time) }}</span>
                  <span class="git-author-cell" :title="row.commit.author_email">{{ row.commit.author }}</span>
                  <span class="git-sha-cell" :title="row.commit.sha">{{ shortCommit(row.commit.sha) }}</span>
                </button>
              </div>
            </div>
            <aside class="commit-detail">
              <template v-if="selectedCommit">
                <h3>{{ selectedCommit.message }}</h3>
                <p>{{ selectedCommit.sha }}</p>
                <div class="meta-line">
                  <span>{{ selectedCommit.author }}</span>
                  <span>{{ formatTime(selectedCommit.commit_time) }}</span>
                </div>
                <h4>变更文件</h4>
                <div class="change-list">
                  <div v-for="file in selectedCommit.files" :key="`${file.status}:${file.path}:${file.old_path}`" class="change-row">
                    <span class="status">{{ file.status }}</span>
                    <span>{{ file.old_path ? `${file.old_path} -> ${file.new_path}` : file.path }}</span>
                  </div>
                </div>
              </template>
              <p v-else class="muted">选择提交查看详情。</p>
            </aside>
          </div>
        </section>

        <section v-if="tab === 'runs' && selectedRepo" class="runs-panel">
          <div class="table">
            <div class="table-head">
              <span>状态</span>
              <span>触发</span>
              <span>分支</span>
              <span>文件</span>
              <span>跳过</span>
              <span>错误</span>
              <span>开始</span>
            </div>
            <div v-for="run in scanRuns" :key="run.id" class="table-line">
              <span class="status" :class="run.status">{{ run.status }}</span>
              <span>{{ run.trigger_type }}</span>
              <span>{{ run.branch_count }}</span>
              <span>{{ run.file_count }}</span>
              <span>{{ run.skipped_count }}</span>
              <span>{{ run.error_count }}</span>
              <span>{{ formatTime(run.started_at) }}</span>
            </div>
          </div>
        </section>

        <section v-if="tab === 'ai'" class="ai-panel">
          <aside class="ai-sidebar">
            <div class="ai-sidebar-head">
              <h3>会话</h3>
              <div class="ai-sidebar-actions">
                <button class="mini-command" type="button" :disabled="aiBusy" title="刷新 AI 问答" @click="loadAIPage">
                  <RefreshCw :size="13" />
                  刷新
                </button>
                <button class="mini-command" type="button" :disabled="aiBusy" @click="startNewAISession">新会话</button>
              </div>
            </div>
            <button
              v-for="session in aiSessions"
              :key="session.id"
              class="ai-session-row"
              :class="{ active: selectedAISession?.id === session.id }"
              type="button"
              @click="selectAISession(session)"
            >
              <span>{{ session.title }}</span>
              <small>{{ formatTime(session.updated_at) }}</small>
            </button>
            <p v-if="!aiSessions.length" class="muted">暂无问答历史。</p>
          </aside>

          <section class="ai-chat">
            <div class="ai-status-line">
              <span class="status" :class="aiSettings?.enabled ? 'active' : 'running'">
                {{ aiSettings?.enabled ? 'AI 已启用' : '本地检索模式' }}
              </span>
              <span>{{ aiSettings?.default_provider_name || '未配置供应商' }}</span>
              <span>{{ aiSettings?.default_model || '-' }}</span>
              <button v-if="!aiSettings?.enabled" class="mini-command" type="button" @click="switchTab('ai-config')">前往 AI 配置</button>
            </div>

            <div v-if="aiPageError" class="inline-alert error">
              <span>{{ aiPageError }}</span>
              <button class="mini-command" type="button" :disabled="aiBusy" @click="loadAIPage">重试</button>
            </div>

            <div ref="messageListRef" class="message-list">
              <article v-for="message in aiMessages" :key="message.id" class="message" :class="message.role">
                <header>
                  <span>{{ message.role === 'user' ? '提问' : '回答' }}</span>
                  <small v-if="message.provider_name">{{ message.provider_name }} · {{ message.model }}</small>
                </header>
                <div v-if="messageNotice(message)" class="message-notice">{{ messageNotice(message)?.message }}</div>
                <div v-if="messageStatusLabel(message)" class="message-state" :class="message.status">
                  <span>{{ messageStatusLabel(message) }}</span>
                  <small v-if="message.error_message">{{ message.error_message }}</small>
                </div>
                <div class="message-content" v-html="renderMessageMarkdown(message.content)"></div>
              </article>
              <div v-if="!aiMessages.length" class="empty-preview">
                <MessageSquare :size="30" />
                <p>从全局仓库提问，DocHarbor 会先做候选服务和证据召回。</p>
              </div>
            </div>

            <form class="ai-composer" @submit.prevent="sendAIQuestion">
              <div class="ai-scope-row">
                <select v-model="aiScopeMode">
                  <option value="global">全部仓库</option>
                  <option value="current_repo" :disabled="!selectedRepo">当前仓库</option>
                  <option value="current_file" :disabled="!canAskWithCurrentFile">当前文件</option>
                </select>
                <span v-if="aiScopeMode === 'current_file' && fileContent" class="scope-hint">
                  {{ fileContent.file_path }}
                </span>
                <label class="inline-check">
                  <input v-model="aiIncludeBranchCandidates" type="checkbox" />
                  功能分支候选
                </label>
              </div>
              <textarea v-model="aiQuestion" rows="4" placeholder="下单页面需要接哪些接口？请求参数和返回字段是什么？"></textarea>
              <div class="composer-actions">
                <button class="primary-action" type="submit" :disabled="aiBusy || !aiQuestion.trim()">
                  <Send :size="16" />
                  发送
                </button>
              </div>
            </form>
          </section>

          <aside class="ai-evidence">
            <div class="section-title-row">
              <h3>证据链</h3>
              <span v-if="aiBusy" class="status running">生成中</span>
            </div>
            <div class="evidence-chain">
              <div v-for="item in aiEvidenceChain" :key="item.id" class="chain-row" :class="[item.kind, item.status]">
                <div>
                  <strong>{{ item.title }}</strong>
                  <small v-if="item.meta">{{ item.meta }}</small>
                </div>
                <p>{{ item.detail }}</p>
              </div>
            </div>
            <p v-if="!aiEvidenceChain.length" class="muted">发送问题后展示检索、路由尝试和证据召回状态。</p>

            <h3>候选服务</h3>
            <div v-for="candidate in aiCandidates" :key="`${candidate.repo_id}:${candidate.service_name}`" class="candidate-row">
              <strong>{{ candidate.service_name }}</strong>
              <span class="status" :class="candidate.confidence">{{ candidate.confidence }}</span>
              <small>{{ candidate.reason }}</small>
            </div>
            <p v-if="!aiCandidates.length" class="muted">发送问题后展示服务候选。</p>

            <h3>引用来源</h3>
            <button
              v-for="citation in aiCitations"
              :key="citation.id || `${citation.repo_id}:${citation.file_path}:${citation.line_start}`"
              class="citation-row"
              type="button"
              @click="openAICitation(citation)"
            >
              <span>{{ citation.repo_name || repoName(citation.repo_id) }}</span>
              <strong>{{ citation.file_path }}</strong>
              <small>{{ citation.source_scope }} · {{ citation.branch }} · {{ shortSha(citation.commit_sha) }} · {{ citation.line_start }}-{{ citation.line_end }}</small>
            </button>
            <p v-if="!aiCitations.length" class="muted">回答引用会保存在会话消息中。</p>
          </aside>
        </section>

        <section v-if="tab === 'ai-config'" class="ai-config-panel">
          <div class="ai-config-head">
            <div>
              <h3>AI 配置</h3>
              <p class="muted">全局 OpenAI-compatible 供应商</p>
            </div>
            <button class="command" type="button" :disabled="aiSettingsLoading" @click="loadAIConfig">
              <RefreshCw :size="16" />
              刷新
            </button>
          </div>

          <div v-if="aiSettingsBanner" class="alert" :class="aiSettingsBanner.type">{{ aiSettingsBanner.message }}</div>

          <section class="config-band">
            <div>
              <h4>当前状态</h4>
              <p>
                <span class="status" :class="aiSettingsStatusClass">{{ aiSettingsStatusLabel }}</span>
                <span v-if="aiSettings?.default_provider_name"> {{ aiSettings.default_provider_name }} · {{ aiSettings.default_model || '-' }}</span>
              </p>
              <p class="muted">
                密钥存储：{{ aiSettings?.encryption_ready ? '本地数据目录已就绪' : '本地数据目录不可写' }}
                <span v-if="aiSettings?.last_test"> · 最近测试：{{ aiSettings.last_test.message }}</span>
              </p>
              <div v-if="aiActiveRouteProviders.length" class="route-chain">
                <span v-for="(provider, index) in aiActiveRouteProviders" :key="provider.provider_key">
                  #{{ index + 1 }} {{ provider.name }} · {{ provider.model }}
                </span>
              </div>
              <div v-if="aiSettings?.has_unapplied_changes" class="route-chain pending">
                <span v-for="(provider, index) in aiSettings.route_providers" :key="provider.provider_key">
                  待应用 #{{ index + 1 }} {{ provider.name }} · {{ provider.model }}
                </span>
              </div>
            </div>
            <button
              v-if="aiSettings?.has_unapplied_changes"
              class="primary-action"
              type="button"
              :disabled="aiApplyDisabled"
              @click="applyAISettingsChanges"
            >
              <Save :size="16" />
              {{ aiApplying ? '应用中' : '应用当前配置' }}
            </button>
          </section>

          <form class="config-form" @submit.prevent="saveAISettings(false)">
            <div class="form-grid">
              <label>
                供应商预设
                <select v-model="aiSelectedPreset" @change="applyAIProviderPreset">
                  <option v-for="preset in aiProviderPresets" :key="preset.key" :value="preset.key">{{ preset.label }}</option>
                </select>
              </label>
              <label>
                供应商名称
                <input v-model="aiSettingsForm.name" @input="markAIFormDirty" />
                <span v-if="aiFieldErrors.name" class="field-error">{{ aiFieldErrors.name }}</span>
              </label>
              <label>
                Base URL
                <input v-model="aiSettingsForm.base_url" placeholder="https://api.deepseek.com" @input="markAIFormDirty" />
                <span v-if="aiFieldErrors.base_url" class="field-error">{{ aiFieldErrors.base_url }}</span>
              </label>
              <label>
                模型
                <input v-model="aiSettingsForm.model" placeholder="deepseek-v4-flash" @input="markAIFormDirty" />
                <span v-if="aiFieldErrors.model" class="field-error">{{ aiFieldErrors.model }}</span>
              </label>
              <label>
                API Key
                <input v-model="aiSettingsForm.api_key" type="password" autocomplete="off" />
                <span v-if="aiFieldErrors.api_key" class="field-error">{{ aiFieldErrors.api_key }}</span>
                <small class="muted">{{ aiAPIKeyHint }}</small>
              </label>
            </div>

            <details class="advanced-settings">
              <summary>高级设置</summary>
              <div class="form-grid">
                <label>
                  超时秒数
                  <input v-model.number="aiSettingsForm.timeout_seconds" type="number" min="5" max="300" @input="markAIFormDirty" />
                  <span v-if="aiFieldErrors.timeout_seconds" class="field-error">{{ aiFieldErrors.timeout_seconds }}</span>
                </label>
                <label>
                  RPM
                  <input v-model.number="aiSettingsForm.max_rpm" type="number" min="1" max="10000" @input="markAIFormDirty" />
                  <span v-if="aiFieldErrors.max_rpm" class="field-error">{{ aiFieldErrors.max_rpm }}</span>
                </label>
                <label>
                  成本等级
                  <select v-model="aiSettingsForm.cost_tier" @change="markAIFormDirty">
                    <option value="low">low</option>
                    <option value="medium">medium</option>
                    <option value="high">high</option>
                  </select>
                </label>
                <label>
                  优先级
                  <input v-model.number="aiSettingsForm.priority" type="number" min="1" max="10000" @input="markAIFormDirty" />
                  <span v-if="aiFieldErrors.priority" class="field-error">{{ aiFieldErrors.priority }}</span>
                </label>
              </div>
            </details>

            <div class="modal-actions">
              <button class="command" type="button" :disabled="aiTestDisabled" @click="testAISettingsProvider">
                <ShieldCheck :size="16" />
                {{ aiTesting ? '测试中' : '测试连接' }}
              </button>
              <button class="command" type="submit" :disabled="aiSaveDisabled">
                <Save :size="16" />
                {{ aiSaving ? '保存中' : '保存' }}
              </button>
              <button class="primary-action" type="button" :disabled="aiEnableDisabled" @click="saveAISettings(true)">
                <Save :size="16" />
                {{ aiEnabling ? '启用中' : '保存并启用' }}
              </button>
              <button class="command" type="button" :disabled="aiDisableDisabled" @click="disableAISettings">
                <X :size="16" />
                {{ aiDisabling ? '停用中' : '停用 AI 问答' }}
              </button>
            </div>
          </form>

          <section v-if="aiSettings?.providers.length" class="config-band provider-summary-band">
            <div>
              <div class="section-title-row">
                <h4>供应商</h4>
                <button class="command" type="button" @click="startNewAIProvider">
                  <Plus :size="16" />
                  新增供应商
                </button>
              </div>
              <div class="provider-list compact">
                <div v-for="provider in aiSettings.providers" :key="provider.provider_key" class="provider-row">
                  <div>
                    <strong>{{ provider.name }}</strong>
                    <p class="muted">{{ provider.base_url }} · {{ provider.model }}</p>
                  </div>
                  <div class="provider-actions">
                    <span v-if="provider.route_order" class="status medium">#{{ provider.route_order }}</span>
                    <span class="status" :class="provider.usable ? 'active' : 'running'">
                      {{ provider.api_key_configured ? `Key 尾号 ${provider.api_key_last4}` : '未配置 Key' }}
                    </span>
                    <span v-if="provider.is_default" class="status active">默认</span>
                    <span class="muted">{{ provider.last_test_message || '未测试' }}</span>
                    <button class="mini-command" type="button" @click="editAIProvider(provider)">编辑</button>
                    <button
                      class="mini-command"
                      type="button"
                      :disabled="aiProviderBusy === provider.provider_key || !provider.api_key_configured"
                      @click="testListedAIProvider(provider)"
                    >
                      测试
                    </button>
                    <button
                      class="mini-command"
                      type="button"
                      :disabled="aiProviderBusy === provider.provider_key || provider.is_default || !provider.usable"
                      @click="makeAIProviderDefault(provider)"
                    >
                      设为默认
                    </button>
                    <button
                      class="mini-command danger"
                      type="button"
                      :disabled="aiProviderBusy === provider.provider_key"
                      @click="deleteAIProvider(provider)"
                    >
                      删除
                    </button>
                  </div>
                </div>
              </div>
            </div>
          </section>
        </section>

        <section v-if="tab === 'ai-diagnostics'" class="ai-diagnostics-panel">
          <div class="ai-config-head">
            <div>
              <h3>AI 诊断</h3>
              <p class="muted">Agent workflow、检索轮次和校验报告</p>
            </div>
            <button class="command" type="button" :disabled="diagnosticsLoading || !diagnosticsToken.trim()" @click="loadDiagnosticsRuns">
              <RefreshCw :size="16" />
              刷新
            </button>
          </div>

          <div v-if="diagnosticsError" class="alert error">{{ diagnosticsError }}</div>

          <section class="config-band diagnostics-controls">
            <label>
              Diagnostics Token
              <input v-model.trim="diagnosticsToken" type="password" autocomplete="off" />
            </label>
            <label>
              状态
              <select v-model="diagnosticsStatus">
                <option value="">全部</option>
                <option value="succeeded">succeeded</option>
                <option value="completed_with_gaps">completed_with_gaps</option>
                <option value="failed">failed</option>
                <option value="insufficient_evidence">insufficient_evidence</option>
              </select>
            </label>
            <label>
              关键词
              <input v-model.trim="diagnosticsQuery" @keydown.enter.prevent="loadDiagnosticsRuns" />
            </label>
            <button class="primary-action" type="button" :disabled="diagnosticsLoading || !diagnosticsToken.trim()" @click="loadDiagnosticsRuns">
              <ShieldCheck :size="16" />
              {{ diagnosticsLoading ? '读取中' : '读取 Runs' }}
            </button>
          </section>

          <section class="diagnostics-grid">
            <aside class="diagnostics-run-list">
              <button
                v-for="item in diagnosticsRuns"
                :key="item.run.id"
                class="diagnostics-run-row"
                :class="{ active: diagnosticsDetail?.run.id === item.run.id }"
                type="button"
                @click="openDiagnosticsRun(item.run.id)"
              >
                <span class="status" :class="item.run.status">{{ item.run.status }}</span>
                <strong>#{{ item.run.id }} {{ item.session.title }}</strong>
                <small>{{ item.user_question }}</small>
                <small>{{ formatTime(item.run.started_at) }} · {{ item.duration_ms }}ms</small>
              </button>
              <p v-if="!diagnosticsRuns.length" class="muted">暂无 run。</p>
            </aside>

            <article class="diagnostics-detail">
              <template v-if="diagnosticsDetail">
                <div class="section-title-row">
                  <div>
                    <h4>Run #{{ diagnosticsDetail.run.id }}</h4>
                    <p class="muted">{{ diagnosticsDetail.user_message.content }}</p>
                  </div>
                  <span class="status" :class="diagnosticsDetail.run.verification_status || diagnosticsDetail.run.status">
                    {{ diagnosticsDetail.run.verification_status || diagnosticsDetail.run.status }}
                  </span>
                </div>

                <div class="diagnostics-summary-grid">
                  <section>
                    <h5>Task Frame</h5>
                    <p><strong>{{ diagnosticsWorkflow?.task_frame?.intent || diagnosticsDetail.run.intent || '-' }}</strong></p>
                    <p>{{ diagnosticsWorkflow?.task_frame?.user_goal || '-' }}</p>
                    <small>{{ listText(diagnosticsWorkflow?.task_frame?.target_artifacts) }}</small>
                  </section>
                  <section>
                    <h5>Evidence Contract</h5>
                    <p><strong>{{ diagnosticsWorkflow?.evidence_contract?.contract_id || '-' }}</strong></p>
                    <small>required: {{ listText(diagnosticsWorkflow?.evidence_contract?.required_keys) }}</small>
                    <small>recommended: {{ listText(diagnosticsWorkflow?.evidence_contract?.recommended_keys) }}</small>
                  </section>
                  <section>
                    <h5>Evidence Bundle</h5>
                    <p><strong>{{ diagnosticsWorkflow?.evidence_bundle?.bundle_id || '-' }}</strong></p>
                    <small>groups: {{ diagnosticsWorkflow?.evidence_bundle?.group_count ?? 0 }}</small>
                    <small>excluded: {{ diagnosticsWorkflow?.evidence_bundle?.excluded_count ?? 0 }}</small>
                  </section>
                  <section>
                    <h5>Answer Verifier</h5>
                    <p><strong>{{ diagnosticsWorkflow?.verification_report?.status || diagnosticsDetail.run.verification_status || '-' }}</strong></p>
                    <small>{{ diagnosticsWorkflow?.verification_report?.next_action || '-' }}</small>
                    <small>{{ listText(diagnosticsWorkflow?.verification_report?.failed_checks) }}</small>
                  </section>
                </div>

                <section class="diagnostics-block">
                  <div class="section-title-row">
                    <h4>Retrieval Rounds</h4>
                    <span class="status medium">{{ diagnosticsWorkflow?.retrieval_rounds?.length || 0 }}</span>
                  </div>
                  <div v-for="round in diagnosticsWorkflow?.retrieval_rounds || []" :key="round.round" class="diagnostics-round">
                    <div>
                      <strong>R{{ round.round }} · {{ round.reason }}</strong>
                      <span>{{ round.new_evidence_count }} new</span>
                    </div>
                    <p>{{ round.searches.map((search) => search.query).join(' · ') }}</p>
                    <small>missing: {{ listText(round.missing_contract_keys) }}</small>
                    <small>delta: {{ coverageDeltaText(round.coverage_delta) }}</small>
                  </div>
                </section>

                <section class="diagnostics-block">
                  <div class="section-title-row">
                    <h4>Contract Coverage</h4>
                    <span class="status" :class="coverageStatusClass(diagnosticsCoverage?.status)">
                      {{ diagnosticsCoverage?.status || '-' }}
                    </span>
                  </div>
                  <div class="coverage-list">
                    <div v-for="item in diagnosticCoverageItems" :key="item.key" class="coverage-row" :class="coverageStatusClass(item.status)">
                      <span>{{ item.status }}</span>
                      <strong>{{ item.key }}</strong>
                      <small>{{ item.reason || item.missing_detail }}</small>
                    </div>
                  </div>
                </section>

                <section class="diagnostics-block">
                  <div class="section-title-row">
                    <h4>Steps</h4>
                    <span class="status medium">{{ diagnosticsDetail.steps.length }}</span>
                  </div>
                  <details v-for="step in diagnosticsDetail.steps" :key="step.id" class="diagnostics-step">
                    <summary>
                      <span class="status" :class="step.status">{{ step.status }}</span>
                      <strong>{{ step.agent_name }}</strong>
                      <small>{{ step.step_type }} · {{ step.latency_ms }}ms</small>
                    </summary>
                    <pre>{{ prettyJSON(diagnosticsStepPayload(step)) }}</pre>
                  </details>
                </section>

                <section class="diagnostics-block">
                  <div class="section-title-row">
                    <h4>Data Sources</h4>
                    <span class="status medium">{{ diagnosticsDetail.data_sources.repositories.length }}</span>
                  </div>
                  <div v-for="source in diagnosticsDetail.data_sources.repositories" :key="source.id" class="diagnostics-source">
                    <strong>{{ source.name }}</strong>
                    <small>{{ source.default_target?.branch || source.default_branch }} · {{ source.latest_scan?.status || 'not_scanned' }}</small>
                    <small>scan: {{ source.scan_paths.map((path) => path.path).join(', ') || '-' }}</small>
                  </div>
                </section>
              </template>
              <div v-else class="empty-preview">
                <ListChecks :size="30" />
                <p>选择 run 查看诊断详情。</p>
              </div>
            </article>
          </section>
        </section>

        <section v-if="tab === 'settings'" class="settings-panel">
          <div class="ai-config-head">
            <div>
              <h3>系统设置</h3>
              <p class="muted">通用访问控制和远程集成</p>
            </div>
          </div>

          <div v-if="settingsBanner" class="alert" :class="settingsBanner.type">{{ settingsBanner.message }}</div>

          <section class="config-band access-token-band">
            <div>
              <h4>访问 Token</h4>
              <p class="muted">签发后可调用授权能力，Token 只显示一次。</p>
              <div class="access-token-form">
                <label>
                  有效期秒数
                  <input v-model.number="accessTokenForm.ttl_seconds" type="number" min="300" max="86400" />
                </label>
                <label>
                  能力
                  <select v-model="accessTokenForm.capabilities" multiple>
                    <option value="ai.history.read">AI 历史读取</option>
                    <option value="ai.diagnostics.read">AI 问答排查</option>
                  </select>
                </label>
                <label>
                  Viewer Key 范围
                  <input v-model.trim="accessTokenForm.viewer_key" placeholder="留空表示不限制 viewer" />
                </label>
              </div>
              <div v-if="issuedAccessToken" class="issued-token">
                <div>
                  <span>过期时间</span>
                  <strong>{{ formatTime(issuedAccessToken.expires_at) }}</strong>
                  <small>{{ issuedAccessToken.capabilities.join(', ') }}</small>
                  <small>{{ issuedAccessToken.scope.viewer_key ? `viewer_key: ${issuedAccessToken.scope.viewer_key}` : '不限制 viewer' }}</small>
                </div>
                <code>{{ issuedAccessToken.token }}</code>
              </div>
            </div>
            <div class="token-actions">
              <button class="primary-action" type="button" :disabled="accessTokenBusy" @click="createAccessToken">
                <ShieldCheck :size="16" />
                {{ accessTokenBusy ? '签发中' : '签发 Token' }}
              </button>
              <button v-if="issuedAccessToken" class="command" type="button" @click="copyAccessToken">
                复制 Token
              </button>
            </div>
          </section>
        </section>
      </section>
    </section>
  </main>
</template>

<script setup lang="ts">
import DOMPurify from 'dompurify'
import {
  Activity,
  Bot,
  BookOpen,
  ChevronLeft,
  Download,
  FileDown,
  FileText,
  Folder,
  GitBranch,
  History,
  ListChecks,
  MessageSquare,
  Plus,
  RefreshCw,
  Save,
  Search,
  Send,
  Settings,
  ShieldCheck,
  SlidersHorizontal,
  X
} from 'lucide-vue-next'
import { marked } from 'marked'
import mermaid from 'mermaid'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { APIRequestError, api, blobURL, downloadURL, htmlPreviewURL, inlineBlobURL } from './api'
import type {
  AICostTier,
  AIContractCoverageItem,
  AIContractCoverageReport,
  AIDiagnosticsRunDetailResponse,
  AIDiagnosticsRunSummary,
  AIDiagnosticsStep,
  AINotice,
  AIMessage,
  AIMessageCitation,
  AccessTokenResponse,
  AIQuestionScope,
  AIServiceCandidate,
  AISettingsForm,
  AISettingsProviderSummary,
  AISettingsSummary,
  AIStreamEvent,
  AISession,
  CommitDetail,
  CommitSummary,
  FileContent,
  FileEntry,
  PathEvent,
  RepoRef,
  Repository,
  ScanRun
} from './types'

type AppTab = 'docs' | 'history' | 'runs' | 'ai' | 'ai-config' | 'ai-diagnostics' | 'settings'
type AIEvidenceChainItem = {
  id: string
  kind: 'stage' | 'provider'
  title: string
  detail: string
  status: string
  meta?: string
}

const repos = ref<Repository[]>([])
const selectedRepo = ref<Repository | null>(null)
const branches = ref<RepoRef[]>([])
const files = ref<FileEntry[]>([])
const selectedFile = ref<FileEntry | null>(null)
const fileContent = ref<FileContent | null>(null)
const htmlViewerContent = ref<FileContent | null>(null)
const htmlViewerError = ref('')
const docEvents = ref<PathEvent[]>([])
const commits = ref<CommitSummary[]>([])
const selectedCommit = ref<CommitDetail | null>(null)
const scanRuns = ref<ScanRun[]>([])
const tab = ref<AppTab>('docs')
const viewMode = ref<'latest' | 'branch'>('latest')
const selectedBranch = ref('')
const historyBranch = ref('')
const currentDir = ref('.')
const searchText = ref('')
const error = ref('')
const busy = ref(false)
const webhookSecret = ref('')
const webhookSecretVisible = ref(false)
const webhookSecretLoading = ref(false)
const showRepoForm = ref(false)
const editingRepo = ref<Repository | null>(null)
const aiSessions = ref<AISession[]>([])
const selectedAISession = ref<AISession | null>(null)
const aiMessages = ref<AIMessage[]>([])
const messageListRef = ref<HTMLElement | null>(null)
const aiQuestion = ref('')
const aiScopeMode = ref<'global' | 'current_repo' | 'current_file'>('global')
const aiIncludeBranchCandidates = ref(true)
const aiBusy = ref(false)
const aiPageError = ref('')
const aiCandidates = ref<AIServiceCandidate[]>([])
const aiCitations = ref<AIMessageCitation[]>([])
const aiEvidenceChain = ref<AIEvidenceChainItem[]>([])
const aiMessageNotices = ref<Record<number, AINotice>>({})
const aiSettings = ref<AISettingsSummary | null>(null)
const aiSettingsForm = ref<AISettingsForm>(defaultAISettingsForm())
const aiSelectedPreset = ref('custom')
const aiFormDirty = ref(false)
const aiFieldErrors = ref<Record<string, string>>({})
const aiSettingsBanner = ref<{ type: 'info' | 'success' | 'warning' | 'error'; message: string } | null>(null)
const settingsBanner = ref<{ type: 'info' | 'success' | 'warning' | 'error'; message: string } | null>(null)
const aiSettingsLoading = ref(false)
const aiSaving = ref(false)
const aiTesting = ref(false)
const aiEnabling = ref(false)
const aiDisabling = ref(false)
const aiApplying = ref(false)
const aiProviderBusy = ref('')
const accessTokenBusy = ref(false)
const accessTokenForm = ref({ ttl_seconds: 3600, capabilities: ['ai.history.read'], viewer_key: '' })
const issuedAccessToken = ref<AccessTokenResponse | null>(null)
const diagnosticsToken = ref('')
const diagnosticsStatus = ref('')
const diagnosticsQuery = ref('')
const diagnosticsRuns = ref<AIDiagnosticsRunSummary[]>([])
const diagnosticsDetail = ref<AIDiagnosticsRunDetailResponse | null>(null)
const diagnosticsLoading = ref(false)
const diagnosticsError = ref('')

const form = ref<Partial<Repository>>({
  name: '',
  slug: '',
  repo_url: '',
  default_branch: 'main',
  sync_interval_seconds: 3600,
  max_file_size_bytes: 2097152
})
const trackedBranchesText = ref('*')
const latestIncludeText = ref('*')
const latestExcludeText = ref('archive/*, tmp/*, dependabot/*')
const branchPriorityText = ref('main, master, release/*, develop, feature/*')
const scanPathsText = ref('.')
const searchResultDir = '搜索结果'
const isHTMLPreviewRoute = window.location.pathname === '/html-preview'
let applyingURLState = false
let repoSelectionRequest = 0
let fileListRequest = 0
let fileOpenRequest = 0
let historyRequest = 0
let aiStreamSequence = 0

interface AIProviderPreset {
  key: string
  label: string
  name: string
  base_url: string
  model: string
  timeout_seconds: number
  max_rpm: number
  cost_tier: AICostTier
  priority: number
}

const aiProviderPresets: AIProviderPreset[] = [
  {
    key: 'deepseek',
    label: 'DeepSeek',
    name: 'DeepSeek',
    base_url: 'https://api.deepseek.com',
    model: 'deepseek-v4-flash',
    timeout_seconds: 60,
    max_rpm: 60,
    cost_tier: 'medium',
    priority: 10
  },
  {
    key: 'openai',
    label: 'OpenAI',
    name: 'OpenAI',
    base_url: 'https://api.openai.com/v1',
    model: '',
    timeout_seconds: 60,
    max_rpm: 60,
    cost_tier: 'medium',
    priority: 10
  },
  {
    key: 'siliconflow',
    label: '硅基流动',
    name: '硅基流动',
    base_url: 'https://api.siliconflow.cn/v1',
    model: '',
    timeout_seconds: 60,
    max_rpm: 60,
    cost_tier: 'medium',
    priority: 10
  },
  {
    key: 'qwen',
    label: '通义千问兼容接口',
    name: '通义千问兼容接口',
    base_url: 'https://dashscope.aliyuncs.com/compatible-mode/v1',
    model: '',
    timeout_seconds: 60,
    max_rpm: 60,
    cost_tier: 'medium',
    priority: 10
  },
  {
    key: 'custom',
    label: '自定义',
    name: '',
    base_url: '',
    model: '',
    timeout_seconds: 60,
    max_rpm: 60,
    cost_tier: 'medium',
    priority: 10
  }
]

function defaultAISettingsForm(): AISettingsForm {
  return {
    provider_key: '',
    name: 'DeepSeek',
    base_url: 'https://api.deepseek.com',
    model: 'deepseek-v4-flash',
    api_key: '',
    timeout_seconds: 60,
    max_rpm: 60,
    priority: 10,
    cost_tier: 'medium'
  }
}

const breadcrumbs = computed(() => {
  if (currentDir.value === '.' || currentDir.value === searchResultDir) return []
  const parts = currentDir.value.split('/').filter(Boolean)
  return parts.map((label, index) => ({
    label,
    path: parts.slice(0, index + 1).join('/')
  }))
})

const canGoParent = computed(() => currentDir.value !== '.' && currentDir.value !== searchResultDir)

const githubWebhookURL = computed(() =>
  selectedRepo.value ? `${window.location.origin}/api/webhooks/github/${selectedRepo.value.id}` : ''
)

const isGlobalTab = computed(() => isGlobalTabName(tab.value))

const workspaceTitle = computed(() => {
  if (tab.value === 'ai') return 'AI 问答'
  if (tab.value === 'ai-config') return 'AI 配置'
  if (tab.value === 'ai-diagnostics') return 'AI 诊断'
  if (tab.value === 'settings') return '系统设置'
  return selectedRepo.value?.name || '仓库配置'
})

const workspaceSubtitle = computed(() => {
  if (tab.value === 'ai') return '全局服务发现、代码证据召回和带引用回答'
  if (tab.value === 'ai-config') return '全局供应商、模型和密钥'
  if (tab.value === 'ai-diagnostics') return '排查 Agent workflow、证据契约和答案校验'
  if (tab.value === 'settings') return '访问 Token 和远程集成'
  return selectedRepo.value?.repo_url || ''
})

const aiCurrentProvider = computed<AISettingsProviderSummary | null>(() => {
  if (!aiSettings.value?.providers.length) return null
  return (
    aiSettings.value.providers.find((provider) => provider.provider_key === aiSettings.value?.default_provider_key) ||
    aiSettings.value.providers[0]
  )
})

const aiHasConfiguredKey = computed(() => Boolean(aiCurrentProvider.value?.api_key_configured))

const aiAPIKeyHint = computed(() => {
  if (aiSettingsForm.value.api_key.trim() && aiHasConfiguredKey.value) return '将替换当前 API Key'
  if (aiSettingsForm.value.api_key.trim()) return '保存后加密存储'
  if (aiCurrentProvider.value?.api_key_configured) return `已配置，尾号 ${aiCurrentProvider.value.api_key_last4 || '****'}`
  return '未配置'
})

const aiSettingsStatusLabel = computed(() => {
  switch (aiSettings.value?.status) {
    case 'enabled':
      return 'AI 问答已启用'
    case 'ready_disabled':
      return '配置可用，尚未启用'
    case 'ready_to_test':
      return '可以测试连接'
    case 'error':
      return '配置错误'
    default:
      return 'AI 尚未启用'
  }
})

const aiSettingsStatusClass = computed(() => {
  switch (aiSettings.value?.status) {
    case 'enabled':
      return 'active'
    case 'ready_disabled':
      return 'medium'
    case 'ready_to_test':
      return 'running'
    case 'error':
      return 'failed'
    default:
      return 'low'
  }
})

const aiFormComplete = computed(
  () => Boolean(aiSettingsForm.value.name.trim() && aiSettingsForm.value.base_url.trim() && aiSettingsForm.value.model.trim())
)

const aiFormHasKey = computed(() => Boolean(aiSettingsForm.value.api_key.trim() || aiHasConfiguredKey.value))

const aiTestDisabled = computed(() => aiTesting.value || !aiSettingsForm.value.base_url.trim() || !aiSettingsForm.value.model.trim() || !aiFormHasKey.value)
const aiSaveDisabled = computed(() => aiSaving.value || !aiFormComplete.value)
const aiEnableDisabled = computed(() => aiEnabling.value || !aiFormComplete.value || !aiFormHasKey.value)
const aiDisableDisabled = computed(() => aiDisabling.value || !aiSettings.value?.enabled)
const aiApplyDisabled = computed(
  () => aiApplying.value || !aiSettings.value?.has_unapplied_changes || !aiSettings.value?.route_provider_keys.length
)

const aiActiveRouteProviders = computed(() => {
  if (!aiSettings.value) return []
  return aiSettings.value.active_route_provider_keys
    .map((key) => aiSettings.value?.providers.find((provider) => provider.provider_key === key))
    .filter((provider): provider is AISettingsProviderSummary => Boolean(provider))
})

const canAskWithCurrentFile = computed(() => Boolean(fileContent.value && fileContent.value.file_size <= 1024 * 1024))

const htmlPreviewSrcdoc = computed(() => (fileContent.value ? buildHTMLPreviewSrcdoc(fileContent.value) : ''))

const htmlViewerSrcdoc = computed(() => (htmlViewerContent.value ? buildHTMLPreviewSrcdoc(htmlViewerContent.value) : ''))

const htmlViewerBackURL = computed(() => {
  const content = htmlViewerContent.value
  if (content) {
    return `/?${new URLSearchParams({
      repo: String(content.repo_id),
      version: String(content.version_id),
      view: 'latest',
      dir: dirNameFromPath(content.file_path)
    })}`
  }
  const params = new URLSearchParams(window.location.search)
  const repoID = Number(params.get('repo') || 0)
  const versionID = Number(params.get('version') || 0)
  if (repoID > 0 && versionID > 0) {
    return `/?${new URLSearchParams({
      repo: String(repoID),
      version: String(versionID),
      view: 'latest'
    })}`
  }
  return '/'
})

const diagnosticsWorkflow = computed(() => diagnosticsDetail.value?.agent_workflow || null)

const diagnosticsCoverage = computed<AIContractCoverageReport | null>(
  () => diagnosticsWorkflow.value?.contract_coverage || diagnosticsDetail.value?.contract_coverage || null
)

const diagnosticCoverageItems = computed<AIContractCoverageItem[]>(() => {
  const coverage = diagnosticsCoverage.value
  if (!coverage) return []
  if (coverage.items?.length) return coverage.items
  const statuses = coverage.coverage || {}
  return Object.entries(statuses).map(([key, status]) => ({
    key,
    requirement: coverage.missing_required?.includes(key) ? 'required' : 'recommended',
    status,
    evidence_ids: [],
    reason: coverage.details?.[key] || '',
    missing_detail: coverage.details?.[key] || '',
    confidence: 0
  }))
})

const graphRowHeight = 38
const graphCenterY = graphRowHeight / 2
const graphLanePadding = 18
const graphLaneGap = 28
const graphPalette = ['#0ea5e9', '#d946ef', '#10b981', '#f59e0b', '#6366f1', '#ef4444']

interface GraphBadge {
  label: string
  type: 'branch' | 'remote' | 'tag' | 'stash' | 'head'
}

interface GraphRow {
  commit: CommitSummary
  lane: number
  beforeLanes: number[]
  afterLanes: number[]
  connectorLanes: number[]
  badges: GraphBadge[]
  isMerge: boolean
  maxLane: number
}

const graphRows = computed(() => buildGraphRows(commits.value))

const graphWidth = computed(() => {
  const maxLane = graphRows.value.reduce((max, row) => Math.max(max, row.maxLane), 0)
  return Math.max(96, graphLanePadding * 2 + (maxLane + 1) * graphLaneGap)
})

const renderedHtml = computed(() => {
  if (!fileContent.value?.content || !isMarkdownContent(fileContent.value)) return ''
  const renderer = new marked.Renderer()
  renderer.image = ({ href, title, text }) => {
    const src = resolveMarkdownResourceURL(href)
    const titleAttr = title ? ` title="${escapeAttr(title)}"` : ''
    return `<img data-doc-harbor-src="${escapeAttr(src)}" alt="${escapeAttr(text || '')}"${titleAttr}>`
  }
  renderer.link = ({ href, title, text }) => {
    const target = resolveMarkdownLinkURL(href)
    const titleAttr = title ? ` title="${escapeAttr(title)}"` : ''
    return `<a href="${escapeAttr(target)}"${titleAttr}>${text}</a>`
  }
  const html = marked.parse(fileContent.value.content, {
    async: false,
    renderer
  }) as string
  return DOMPurify.sanitize(html, {
    ADD_TAGS: ['svg', 'path', 'g', 'marker', 'defs', 'linearGradient', 'stop', 'rect', 'circle', 'line', 'polyline', 'polygon', 'text', 'tspan'],
    ADD_ATTR: ['data-doc-harbor-src', 'viewBox', 'd', 'x', 'y', 'x1', 'x2', 'y1', 'y2', 'points', 'marker-end', 'stroke', 'fill', 'transform', 'class', 'style', 'target', 'rel']
  })
})

watch(renderedHtml, async () => {
  await nextTick()
  armMarkdownImageRetries()
  renderMermaid()
})

onMounted(async () => {
  mermaid.initialize({
    startOnLoad: false,
    securityLevel: 'strict',
    theme: 'default',
    htmlLabels: false,
    themeVariables: {
      actorBkg: '#1f2937',
      actorBorder: '#38bdf8',
      actorLineColor: '#38bdf8',
      actorTextColor: '#f8fafc',
      activationBkgColor: '#e0f2fe',
      activationBorderColor: '#0ea5e9',
      labelBoxBkgColor: '#ffffff',
      labelBoxBorderColor: '#94a3b8',
      labelTextColor: '#1f2937',
      loopTextColor: '#1f2937',
      noteBkgColor: '#fff7ed',
      noteBorderColor: '#fb923c',
      noteTextColor: '#1f2937',
      signalColor: '#64748b',
      signalTextColor: '#1f2937',
      sequenceNumberColor: '#ffffff'
    }
  })
  if (isHTMLPreviewRoute) {
    await loadHTMLViewer()
    return
  }
  await loadAll()
  window.addEventListener('popstate', applyURLState)
})

onBeforeUnmount(() => {
  if (isHTMLPreviewRoute) return
  window.removeEventListener('popstate', applyURLState)
})

async function loadHTMLViewer() {
  htmlViewerError.value = ''
  const params = new URLSearchParams(window.location.search)
  const repoID = Number(params.get('repo') || 0)
  const versionID = Number(params.get('version') || 0)
  if (repoID <= 0 || versionID <= 0) {
    htmlViewerError.value = '缺少 repo 或 version 参数'
    return
  }
  try {
    const content = await api.content(repoID, versionID)
    if (!content.previewable) {
      htmlViewerError.value = '当前文件不可预览'
      htmlViewerContent.value = content
      return
    }
    if (!isHTMLContent(content)) {
      htmlViewerError.value = '当前文件不是 HTML 文件'
      htmlViewerContent.value = content
      return
    }
    if (!content.content) {
      htmlViewerError.value = 'HTML 内容为空'
      htmlViewerContent.value = content
      return
    }
    htmlViewerContent.value = content
  } catch (err) {
    htmlViewerError.value = err instanceof Error ? err.message : String(err)
  }
}

async function loadAll() {
  await withBusy(async () => {
    const response = await api.repos()
    repos.value = response.items
    const urlState = readURLState()
    if (isGlobalTabName(urlState.tab)) {
      tab.value = urlState.tab
      if (selectedRepo.value) {
        const refreshed = repos.value.find((repo) => repo.id === selectedRepo.value?.id)
        if (refreshed) selectedRepo.value = refreshed
      }
      await loadGlobalTabData(urlState.tab)
      return
    }
    if (!repos.value.length) return
    if (applyingURLState) {
      const refreshed = selectedRepo.value ? repos.value.find((repo) => repo.id === selectedRepo.value?.id) : null
      if (refreshed) selectedRepo.value = refreshed
      return
    }
    const urlRepo = urlState.repoID ? repos.value.find((repo) => repo.id === urlState.repoID) : null
    if (!selectedRepo.value) {
      await selectRepo(urlRepo || repos.value[0], { state: urlState, syncURL: !urlRepo })
    } else if (selectedRepo.value) {
      const refreshed = repos.value.find((repo) => repo.id === selectedRepo.value?.id)
      if (refreshed) selectedRepo.value = refreshed
    }
  })
}

async function openRepo(repo: Repository) {
  if (isGlobalTab.value) tab.value = 'docs'
  await selectRepo(repo)
}

async function selectRepo(repo: Repository, options: { state?: URLState; syncURL?: boolean } = {}) {
  const requestID = ++repoSelectionRequest
  fileListRequest++
  fileOpenRequest++
  historyRequest++
  selectedRepo.value = repo
  resetWebhookSecret()
  fileContent.value = null
  selectedFile.value = null
  docEvents.value = []
  await loadBranches()
  if (requestID !== repoSelectionRequest) return
  const state = options.state
  if (state) {
    tab.value = state.tab
    viewMode.value = state.view
    if (state.branch && branches.value.some((branch) => branch.ref_name === state.branch)) {
      selectedBranch.value = state.branch
    }
    historyBranch.value = state.branch && branches.value.some((branch) => branch.ref_name === state.branch) ? state.branch : ''
  }
  const initialDir = state?.dir !== undefined ? state.dir : repoEntryDir(repo)
  await loadFiles(initialDir, { syncURL: false })
  if (requestID !== repoSelectionRequest) return
  if (state?.versionID) {
    await openVersion(state.versionID, { syncURL: false })
    if (requestID !== repoSelectionRequest) return
  }
  if (tab.value === 'history') await loadHistory()
  if (requestID !== repoSelectionRequest) return
  if (tab.value === 'runs') await loadScanRuns()
  if (requestID !== repoSelectionRequest) return
  if (options.syncURL !== false) updateURL()
}

async function loadBranches() {
  if (!selectedRepo.value) return
  const repoID = selectedRepo.value.id
  const response = await api.branches(repoID)
  if (selectedRepo.value?.id !== repoID) return
  branches.value = response.items
  const defaultBranch = selectedRepo.value.default_branch
  selectedBranch.value = branches.value.find((branch) => branch.ref_name === defaultBranch)?.ref_name || branches.value[0]?.ref_name || ''
  historyBranch.value = ''
}

async function loadFiles(dir: string, options: { syncURL?: boolean } = {}) {
  if (!selectedRepo.value) return
  const requestID = ++fileListRequest
  const repoID = selectedRepo.value.id
  fileOpenRequest++
  historyRequest++
  currentDir.value = dir
  selectedFile.value = null
  fileContent.value = null
  docEvents.value = []
  const params: Record<string, string> = { view: viewMode.value, dir }
  if (viewMode.value === 'branch') params.branch = selectedBranch.value
  const response = await api.files(repoID, params)
  if (requestID !== fileListRequest || selectedRepo.value?.id !== repoID) return
  files.value = response.items
  if (dir !== searchResultDir) searchText.value = ''
  if (options.syncURL !== false) updateURL({ clearVersion: true })
}

async function loadBranchFiles() {
  await loadFiles(repoEntryDir(selectedRepo.value))
}

async function runSearch() {
  if (!selectedRepo.value || !searchText.value.trim()) {
    await loadFiles(currentDir.value)
    return
  }
  const requestID = ++fileListRequest
  const repoID = selectedRepo.value.id
  fileOpenRequest++
  historyRequest++
  selectedFile.value = null
  fileContent.value = null
  docEvents.value = []
  const response = await api.search(repoID, searchText.value.trim())
  if (requestID !== fileListRequest || selectedRepo.value?.id !== repoID) return
  files.value = response.items
  currentDir.value = searchResultDir
  updateURL({ clearVersion: true })
}

async function switchView(mode: 'latest' | 'branch') {
  viewMode.value = mode
  await loadFiles(repoEntryDir(selectedRepo.value))
}

async function openFile(entry: FileEntry, options: { syncURL?: boolean } = {}) {
  if (!selectedRepo.value || !entry.version_id) return
  selectedFile.value = entry
  await openVersion(entry.version_id, options)
}

async function openVersion(versionID: number, options: { syncURL?: boolean } = {}) {
  if (!selectedRepo.value) return
  const requestID = ++fileOpenRequest
  const repoID = selectedRepo.value.id
  const content = await api.content(repoID, versionID)
  if (requestID !== fileOpenRequest || selectedRepo.value?.id !== repoID) return
  fileContent.value = content
  selectedFile.value = files.value.find((entry) => entry.kind === 'file' && entry.version_id === versionID) || selectedFile.value
  await loadDocHistory(content.document_id)
  if (requestID !== fileOpenRequest || selectedRepo.value?.id !== repoID) return
  if (options.syncURL !== false) updateURL()
}

async function loadDocHistory(documentID: number) {
  if (!selectedRepo.value) return
  const requestID = ++historyRequest
  const repoID = selectedRepo.value.id
  const history = await api.docHistory(repoID, documentID)
  if (requestID !== historyRequest || selectedRepo.value?.id !== repoID || fileContent.value?.document_id !== documentID) return
  docEvents.value = Array.isArray(history.events) ? history.events : []
}

async function scanSelected() {
  if (!selectedRepo.value) return
  await withBusy(async () => {
    await api.scanRepo(selectedRepo.value!.id)
    await loadAll()
    await loadFiles(currentDir.value === searchResultDir ? '.' : currentDir.value)
  })
}

async function switchTab(nextTab: AppTab) {
  tab.value = nextTab
  updateURL({ clearVersion: nextTab !== 'docs' })
  if (nextTab === 'history') await loadHistory()
  if (nextTab === 'runs') await loadScanRuns()
  await loadGlobalTabData(nextTab)
}

async function goParent() {
  if (!canGoParent.value) return
  const parent = parentDir(currentDir.value)
  await loadFiles(parent)
}

function editSelected() {
  if (!selectedRepo.value) return
  editingRepo.value = selectedRepo.value
  form.value = { ...selectedRepo.value }
  trackedBranchesText.value = selectedRepo.value.tracked_branches.join(', ')
  latestIncludeText.value = selectedRepo.value.latest_include_branches.join(', ')
  latestExcludeText.value = selectedRepo.value.latest_exclude_branches.join(', ')
  branchPriorityText.value = selectedRepo.value.branch_priority.join(', ')
  scanPathsText.value = selectedRepo.value.scan_paths.map((p) => p.path).join('\n') || '.'
  showRepoForm.value = true
}

async function saveRepo() {
  const payload: Partial<Repository> = {
    ...form.value,
    tracked_branches: splitList(trackedBranchesText.value),
    latest_include_branches: splitList(latestIncludeText.value),
    latest_exclude_branches: splitList(latestExcludeText.value),
    branch_priority: splitList(branchPriorityText.value),
    enabled: true,
    scan_paths: scanPathsText.value
      .split('\n')
      .map((path) => path.trim())
      .filter(Boolean)
      .map((path) => ({ path, include_globs: [], exclude_globs: [], enabled: true }))
  }
  await withBusy(async () => {
    if (editingRepo.value?.id) {
      const updated = await api.updateRepo(editingRepo.value.id, payload)
      selectedRepo.value = updated
    } else {
      const created = await api.createRepo(payload)
      selectedRepo.value = created
    }
    showRepoForm.value = false
    editingRepo.value = null
    resetForm()
    await loadAll()
    if (selectedRepo.value) {
      await selectRepo(selectedRepo.value)
    }
  })
}

async function loadHistory() {
  if (!selectedRepo.value) return
  const response = await api.history(selectedRepo.value.id, historyBranch.value, 120)
  commits.value = response.items
  selectedCommit.value = null
  if (commits.value[0]) {
    await openCommit(commits.value[0].sha)
  }
}

async function changeHistoryBranch() {
  await loadHistory()
  updateURL({ clearVersion: true })
}

async function openCommit(sha: string) {
  if (!selectedRepo.value) return
  selectedCommit.value = await api.commit(selectedRepo.value.id, sha)
}

async function loadScanRuns() {
  if (!selectedRepo.value) return
  const response = await api.scanRuns(selectedRepo.value.id)
  scanRuns.value = response.items
}

async function loadDiagnosticsRuns() {
  if (!diagnosticsToken.value.trim()) return
  diagnosticsLoading.value = true
  diagnosticsError.value = ''
  try {
    const params: Record<string, string> = { limit: '50' }
    if (diagnosticsStatus.value) params.status = diagnosticsStatus.value
    if (diagnosticsQuery.value) params.q = diagnosticsQuery.value
    const response = await api.aiDiagnosticsRuns(diagnosticsToken.value.trim(), params)
    diagnosticsRuns.value = response.items || []
    if (diagnosticsRuns.value[0]) {
      await openDiagnosticsRun(diagnosticsRuns.value[0].run.id)
    } else {
      diagnosticsDetail.value = null
    }
  } catch (err) {
    diagnosticsError.value = err instanceof Error ? err.message : String(err)
  } finally {
    diagnosticsLoading.value = false
  }
}

async function openDiagnosticsRun(runID: number) {
  if (!diagnosticsToken.value.trim()) return
  diagnosticsLoading.value = true
  diagnosticsError.value = ''
  try {
    diagnosticsDetail.value = await api.aiDiagnosticsRun(diagnosticsToken.value.trim(), runID)
  } catch (err) {
    diagnosticsError.value = err instanceof Error ? err.message : String(err)
  } finally {
    diagnosticsLoading.value = false
  }
}

async function loadAIPage() {
  aiBusy.value = true
  error.value = ''
  aiPageError.value = ''
  try {
    const [settings, response] = await Promise.all([api.aiSettings(), api.aiSessions({ limit: '50' })])
    aiSettings.value = settings
    aiSessions.value = response.items || []
    if (!selectedAISession.value && aiSessions.value[0]) {
      await selectAISession(aiSessions.value[0])
    } else if (selectedAISession.value) {
      const refreshed = aiSessions.value.find((session) => session.id === selectedAISession.value?.id)
      if (refreshed) {
        selectedAISession.value = refreshed
        await loadAIMessages()
      } else {
        selectedAISession.value = null
        aiMessages.value = []
        aiCandidates.value = []
        aiCitations.value = []
        aiEvidenceChain.value = []
      }
    } else {
      aiMessages.value = []
      aiCandidates.value = []
      aiCitations.value = []
      aiEvidenceChain.value = []
    }
  } catch (err) {
    aiPageError.value = err instanceof Error ? err.message : String(err)
  } finally {
    aiBusy.value = false
  }
}

async function createNewAISession(options: { manageBusy?: boolean } = {}) {
  const manageBusy = options.manageBusy !== false
  if (manageBusy) aiBusy.value = true
  aiPageError.value = ''
  try {
    const session = await api.createAISession({ title: '新的 AI 问答', scope: buildAIScope() })
    selectedAISession.value = session
    aiSessions.value = [session, ...aiSessions.value.filter((item) => item.id !== session.id)]
    aiMessages.value = []
    aiCandidates.value = []
    aiCitations.value = []
    aiEvidenceChain.value = []
    return session
  } catch (err) {
    aiPageError.value = err instanceof Error ? err.message : String(err)
    return null
  } finally {
    if (manageBusy) aiBusy.value = false
  }
}

async function startNewAISession() {
  await createNewAISession()
}

async function selectAISession(session: AISession) {
  selectedAISession.value = session
  aiCandidates.value = []
  aiCitations.value = []
  aiEvidenceChain.value = []
  await loadAIMessages()
}

async function loadAIMessages() {
  if (!selectedAISession.value) {
    aiMessages.value = []
    aiCandidates.value = []
    aiCitations.value = []
    aiEvidenceChain.value = []
    return
  }
  const response = await api.aiMessages(selectedAISession.value.id)
  aiMessages.value = response.items || []
  aiCandidates.value = response.service_candidates || []
  aiCitations.value = response.citations || []
}

async function sendAIQuestion() {
  if (!aiQuestion.value.trim() || aiBusy.value) return
  aiBusy.value = true
  aiPageError.value = ''
  try {
    if (!selectedAISession.value) {
      await createNewAISession({ manageBusy: false })
    }
    if (!selectedAISession.value) return
    const question = aiQuestion.value.trim()
    aiQuestion.value = ''
    aiCandidates.value = []
    aiCitations.value = []
    aiEvidenceChain.value = []
    await api.streamAI(selectedAISession.value.id, question, buildAIScope(), handleAIStreamEvent)
    await refreshAISessions()
  } catch (err) {
    aiPageError.value = err instanceof Error ? err.message : String(err)
  } finally {
    aiBusy.value = false
  }
}

async function handleAIStreamEvent(event: AIStreamEvent) {
  switch (event.type) {
    case 'user_message':
      upsertAIMessage(event.message)
      addEvidenceChainItem({
        kind: 'stage',
        title: '问题已入库',
        detail: '用户问题已保存到当前会话。',
        status: 'success'
      })
      break
    case 'run_started':
      upsertAIMessage(event.assistant_message)
      addEvidenceChainItem({
        kind: 'stage',
        title: '运行已开始',
        detail: `run #${event.run.id} 已创建，等待检索和模型路由。`,
        status: 'running'
      })
      break
    case 'task_frame':
      addEvidenceChainItem({
        kind: 'stage',
        title: '任务帧',
        detail: `${event.task_frame.intent} · ${event.task_frame.answer_shape}`,
        status: 'success',
        meta: event.task_frame.target_artifacts.slice(0, 4).join(' · ')
      })
      break
    case 'contract':
      addEvidenceChainItem({
        kind: 'stage',
        title: '证据契约',
        detail: `${event.contract.contract_id} · required: ${event.contract.required_keys.join(', ')}`,
        status: 'success',
        meta: event.contract.recommended_keys?.length ? `recommended: ${event.contract.recommended_keys.join(', ')}` : ''
      })
      break
    case 'retrieval_round':
      addEvidenceChainItem({
        kind: 'stage',
        title: `检索 R${event.round.round}`,
        detail: event.round.reason,
        status: 'success',
        meta: `新增 ${event.round.new_evidence_count} · ${coverageDeltaText(event.round.coverage_delta)}`
      })
      break
    case 'coverage':
      addEvidenceChainItem({
        kind: 'stage',
        title: '契约覆盖',
        detail: `${event.coverage.status} · next: ${event.coverage.next_action}`,
        status: coverageStatusClass(event.coverage.status),
        meta: `缺口 ${event.coverage.unconfirmed_count ?? event.coverage.missing_required?.length ?? 0} · 证据组 ${event.evidence_bundle?.group_count ?? 0}`
      })
      break
    case 'stage':
      addEvidenceChainItem({
        kind: 'stage',
        title: aiStageTitle(event.stage),
        detail: event.message,
        status: event.status,
        meta: [event.candidate_count ? `候选 ${event.candidate_count}` : '', event.evidence_count ? `引用 ${event.evidence_count}` : '']
          .filter(Boolean)
          .join(' · ')
      })
      break
    case 'service_candidates':
      aiCandidates.value = event.items || []
      break
    case 'citations':
      aiCitations.value = event.items || []
      break
    case 'provider_attempt':
      addEvidenceChainItem({
        kind: 'provider',
        title: event.provider || event.provider_key,
        detail: event.error ? `${aiStreamStatusLabel(event.status)}：${event.error}` : aiStreamStatusLabel(event.status),
        status: event.status,
        meta: `${event.model || '-'} · 优先级 ${event.priority}`
      })
      break
    case 'verification':
      addEvidenceChainItem({
        kind: 'stage',
        title: '答案校验',
        detail: event.report.reason
          ? `${event.report.status} · ${event.report.reason}`
          : `${event.report.status} · ${event.report.next_action}`,
        status: event.report.status === 'pass' ? 'success' : event.report.status,
        meta: event.report.failed_checks?.join(' · ') || ''
      })
      break
    case 'answer_delta':
      appendAIAnswerDelta(event.message_id, event.delta)
      break
    case 'message_done':
      upsertAIMessage(event.message)
      aiCandidates.value = event.service_candidates || []
      aiCitations.value = event.citations || []
      if (event.notice && event.message?.id) {
        aiMessageNotices.value[event.message.id] = event.notice
      }
      addEvidenceChainItem({
        kind: 'stage',
        title: '回答完成',
        detail: `答案已保存，输入 ${event.usage?.prompt_tokens || 0} tokens，输出 ${event.usage?.completion_tokens || 0} tokens。`,
        status: 'success'
      })
      break
    case 'error':
      if (event.assistant_message) {
        upsertAIMessage(event.assistant_message)
      } else if (event.partial_message_id) {
        const message = aiMessages.value.find((item) => item.id === event.partial_message_id)
        if (message) {
          message.status = message.content ? 'partial' : 'failed'
          message.error_message = event.message
        }
      }
      aiPageError.value = event.message
      addEvidenceChainItem({
        kind: 'stage',
        title: '流式回答中断',
        detail: event.message,
        status: 'failed'
      })
      break
    case 'done':
      break
  }
  await scrollAIToBottom()
}

function upsertAIMessage(message: AIMessage) {
  const index = aiMessages.value.findIndex((item) => item.id === message.id)
  if (index >= 0) {
    aiMessages.value[index] = message
  } else {
    aiMessages.value.push(message)
  }
}

function appendAIAnswerDelta(messageID: number, delta: string) {
  let message = aiMessages.value.find((item) => item.id === messageID)
  if (!message) {
    message = {
      id: messageID,
      session_id: selectedAISession.value?.id || 0,
      role: 'assistant',
      content: '',
      model: '',
      provider_name: '',
      model_route_json: '',
      prompt_tokens: 0,
      completion_tokens: 0,
      latency_ms: 0,
      status: 'streaming',
      error_message: '',
      created_at: ''
    }
    aiMessages.value.push(message)
  }
  message.content += delta
  message.status = 'streaming'
}

function addEvidenceChainItem(item: Omit<AIEvidenceChainItem, 'id'>) {
  aiStreamSequence += 1
  aiEvidenceChain.value.push({ id: `${Date.now()}:${aiStreamSequence}`, ...item })
}

async function scrollAIToBottom() {
  await nextTick()
  if (messageListRef.value) {
    messageListRef.value.scrollTop = messageListRef.value.scrollHeight
  }
}

async function refreshAISessions() {
  const response = await api.aiSessions({ limit: '50' })
  aiSessions.value = response.items || []
  if (selectedAISession.value) {
    const refreshed = aiSessions.value.find((session) => session.id === selectedAISession.value?.id)
    if (refreshed) selectedAISession.value = refreshed
  }
}

function aiStageTitle(stage: string) {
  switch (stage) {
    case 'retrieve_smart_latest':
      return '检索与证据召回'
    case 'model_call':
      return '模型调用'
    case 'verify_answer':
      return '答案校验'
    default:
      return stage || '阶段状态'
  }
}

function aiStreamStatusLabel(status: string) {
  switch (status) {
    case 'started':
      return '开始尝试'
    case 'running':
      return '进行中'
    case 'succeeded':
    case 'success':
      return '成功'
    case 'failed':
      return '失败'
    case 'skipped':
      return '跳过'
    default:
      return status || '状态更新'
  }
}

function buildAIScope(): AIQuestionScope {
  let repoMode = aiScopeMode.value
  if (repoMode === 'current_repo' && !selectedRepo.value) repoMode = 'global'
  if (repoMode === 'current_file' && !fileContent.value) repoMode = 'global'
  const scope: AIQuestionScope = {
    repo_mode: repoMode,
    repo_ids: [],
    source_mode: aiIncludeBranchCandidates.value ? 'smart_latest_with_branch_candidates' : 'smart_latest',
    file_types: ['all']
  }
  if (repoMode === 'current_repo' && selectedRepo.value) {
    scope.repo_ids = [selectedRepo.value.id]
  }
  if (repoMode === 'current_file' && fileContent.value) {
    scope.repo_ids = [fileContent.value.repo_id]
    scope.current_file = {
      repo_id: fileContent.value.repo_id,
      version_id: fileContent.value.version_id,
      branch: fileContent.value.branch,
      commit_sha: fileContent.value.source_commit_sha,
      file_path: fileContent.value.file_path
    }
  }
  return scope
}

async function askWithCurrentFile() {
  if (!fileContent.value || !canAskWithCurrentFile.value) return
  aiScopeMode.value = 'current_file'
  aiQuestion.value = `基于当前文件 ${fileContent.value.file_path} 回答：`
  await switchTab('ai')
}

async function openAICitation(citation: AIMessageCitation) {
  const repo = repos.value.find((item) => item.id === citation.repo_id)
  if (repo) {
    await selectRepo(repo, { syncURL: false })
  } else {
    aiPageError.value = `未找到引用所属仓库：${citation.repo_name || citation.repo_id}`
    return
  }
  tab.value = 'docs'
  if (citation.version_id > 0) {
    currentDir.value = parentDir(citation.file_path)
    await loadFiles(currentDir.value, { syncURL: false })
    try {
      await openVersion(citation.version_id)
    } catch (err) {
      error.value = err instanceof Error ? err.message : String(err)
    }
  } else {
    currentDir.value = parentDir(citation.file_path)
    await loadFiles(currentDir.value)
  }
}

async function loadAIConfig() {
  aiSettingsLoading.value = true
  aiSettingsBanner.value = null
  aiFieldErrors.value = {}
  error.value = ''
  try {
    const settings = await api.aiSettings()
    aiSettings.value = settings
    applyAISettingsToForm(settings)
    if (settings.status === 'error') {
      aiSettingsBanner.value = { type: 'error', message: settings.last_test?.message || '配置需要修复' }
    }
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    aiSettingsLoading.value = false
  }
}

function applyAISettingsToForm(settings: AISettingsSummary) {
  const provider = settings.providers.find((item) => item.provider_key === settings.default_provider_key) || settings.providers[0]
  if (provider) {
    aiSettingsForm.value = {
      provider_key: provider.provider_key,
      name: provider.name,
      base_url: provider.base_url,
      model: provider.model,
      api_key: '',
      timeout_seconds: provider.timeout_seconds || provider.request_timeout_seconds || 60,
      max_rpm: provider.max_rpm || 60,
      priority: provider.priority || 10,
      cost_tier: provider.cost_tier || 'medium'
    }
    aiSelectedPreset.value = detectAIProviderPreset(provider)
  } else {
    aiSettingsForm.value = defaultAISettingsForm()
    aiSelectedPreset.value = 'deepseek'
  }
  aiFormDirty.value = false
}

function detectAIProviderPreset(provider: Pick<AISettingsProviderSummary, 'name' | 'base_url'>) {
  const matched = aiProviderPresets.find(
    (preset) => preset.key !== 'custom' && preset.name === provider.name && preset.base_url === provider.base_url
  )
  return matched?.key || 'custom'
}

function markAIFormDirty() {
  aiFormDirty.value = true
}

function applyAIProviderPreset() {
  const preset = aiProviderPresets.find((item) => item.key === aiSelectedPreset.value)
  if (!preset) return
  const overwrite = !aiFormDirty.value
  const form = aiSettingsForm.value
  if (overwrite || !form.name.trim()) form.name = preset.name
  if (overwrite || !form.base_url.trim()) form.base_url = preset.base_url
  if (overwrite || !form.model.trim()) form.model = preset.model
  if (overwrite) {
    form.timeout_seconds = preset.timeout_seconds
    form.max_rpm = preset.max_rpm
    form.cost_tier = preset.cost_tier
    form.priority = preset.priority
  }
  aiFormDirty.value = false
}

async function testAISettingsProvider() {
  aiTesting.value = true
  aiFieldErrors.value = {}
  aiSettingsBanner.value = null
  try {
    const result = await api.testAIProvider({ ...aiSettingsForm.value })
    if (result.status === 'pass') {
      aiSettingsBanner.value = { type: 'success', message: `${result.message} · ${result.latency_ms}ms` }
    } else {
      aiSettingsBanner.value = { type: 'warning', message: result.message || result.safe_error || '连接测试失败' }
    }
  } catch (err) {
    handleAISettingsError(err)
  } finally {
    aiTesting.value = false
  }
}

async function saveAISettings(enable: boolean) {
  if (enable) aiEnabling.value = true
  else aiSaving.value = true
  aiFieldErrors.value = {}
  aiSettingsBanner.value = null
  try {
    const creatingAdditionalProvider = !aiSettingsForm.value.provider_key && Boolean(aiSettings.value?.providers.length)
    const response =
      creatingAdditionalProvider && !enable
        ? await api.createAIProvider({ ...aiSettingsForm.value })
        : creatingAdditionalProvider && enable
          ? await saveAndApplyNewAIProvider()
          : await api.saveAIDefaultProvider({ ...aiSettingsForm.value, enable })
    aiSettings.value = response.settings
    aiSettingsForm.value.api_key = ''
    applyAISettingsToForm(response.settings)
    aiSettingsBanner.value = { type: enable ? 'success' : 'info', message: response.message }
  } catch (err) {
    handleAISettingsError(err)
  } finally {
    aiSaving.value = false
    aiEnabling.value = false
  }
}

async function saveAndApplyNewAIProvider() {
  const created = await api.createAIProvider({ ...aiSettingsForm.value, test_before_save: true })
  aiSettings.value = created.settings
  const applied = await api.applyAISettings({ enabled: true, test_policy: 'default_only' })
  return { ...applied, provider: created.provider }
}

async function disableAISettings() {
  aiDisabling.value = true
  aiSettingsBanner.value = null
  try {
    const response = await api.setAIEnabled(false)
    aiSettings.value = response.settings
    applyAISettingsToForm(response.settings)
    aiSettingsBanner.value = { type: 'info', message: 'AI 问答已停用' }
  } catch (err) {
    handleAISettingsError(err)
  } finally {
    aiDisabling.value = false
  }
}

async function applyAISettingsChanges() {
  aiApplying.value = true
  aiSettingsBanner.value = null
  try {
    const response = await api.applyAISettings({ enabled: true, test_policy: 'default_only' })
    aiSettings.value = response.settings
    applyAISettingsToForm(response.settings)
    aiSettingsBanner.value = { type: 'success', message: response.message }
  } catch (err) {
    handleAISettingsError(err)
  } finally {
    aiApplying.value = false
  }
}

async function createAccessToken() {
  accessTokenBusy.value = true
  settingsBanner.value = null
  issuedAccessToken.value = null
  try {
    const ttl = Number(accessTokenForm.value.ttl_seconds) || 3600
    const response = await api.createAccessToken({
      ttl_seconds: ttl,
      capabilities: accessTokenForm.value.capabilities,
      scope: { viewer_key: accessTokenForm.value.viewer_key.trim() || undefined }
    })
    issuedAccessToken.value = response
    settingsBanner.value = { type: 'success', message: '访问 Token 已签发' }
  } catch (err) {
    settingsBanner.value = { type: 'error', message: err instanceof Error ? err.message : String(err) }
  } finally {
    accessTokenBusy.value = false
  }
}

async function copyAccessToken() {
  if (!issuedAccessToken.value?.token) return
  settingsBanner.value = null
  try {
    await navigator.clipboard.writeText(issuedAccessToken.value.token)
    settingsBanner.value = { type: 'success', message: 'Token 已复制' }
  } catch (err) {
    settingsBanner.value = { type: 'error', message: err instanceof Error ? err.message : String(err) }
  }
}

function startNewAIProvider() {
  aiSettingsForm.value = defaultAISettingsForm()
  aiSelectedPreset.value = 'deepseek'
  aiFormDirty.value = false
  aiFieldErrors.value = {}
  aiSettingsBanner.value = { type: 'info', message: '填写后点击保存会新增供应商，应用后进入问答转移链。' }
}

function editAIProvider(provider: AISettingsProviderSummary) {
  aiSettingsForm.value = {
    provider_key: provider.provider_key,
    name: provider.name,
    base_url: provider.base_url,
    model: provider.model,
    api_key: '',
    timeout_seconds: provider.timeout_seconds || provider.request_timeout_seconds || 60,
    max_rpm: provider.max_rpm || 60,
    priority: provider.priority || 10,
    cost_tier: provider.cost_tier || 'medium'
  }
  aiSelectedPreset.value = detectAIProviderPreset(provider)
  aiFormDirty.value = false
  aiFieldErrors.value = {}
}

async function testListedAIProvider(provider: AISettingsProviderSummary) {
  aiProviderBusy.value = provider.provider_key
  aiSettingsBanner.value = null
  try {
    const result = await api.testAIProvider({
      provider_key: provider.provider_key,
      name: provider.name,
      base_url: provider.base_url,
      model: provider.model,
      timeout_seconds: provider.timeout_seconds || provider.request_timeout_seconds || 20
    })
    aiSettingsBanner.value =
      result.status === 'pass'
        ? { type: 'success', message: `${provider.name} ${result.message} · ${result.latency_ms}ms` }
        : { type: 'warning', message: result.message || result.safe_error || '连接测试失败' }
  } catch (err) {
    handleAISettingsError(err)
  } finally {
    aiProviderBusy.value = ''
  }
}

async function makeAIProviderDefault(provider: AISettingsProviderSummary) {
  aiProviderBusy.value = provider.provider_key
  aiSettingsBanner.value = null
  try {
    const response = await api.updateAIProvider(provider.provider_key, { make_default: true })
    aiSettings.value = response.settings
    applyAISettingsToForm(response.settings)
    aiSettingsBanner.value = { type: 'info', message: response.message }
  } catch (err) {
    handleAISettingsError(err)
  } finally {
    aiProviderBusy.value = ''
  }
}

async function deleteAIProvider(provider: AISettingsProviderSummary) {
  const replacement = aiSettings.value?.providers.find((item) => item.provider_key !== provider.provider_key && item.usable)
  if (provider.is_default && aiSettings.value?.enabled && !replacement) {
    aiSettingsBanner.value = { type: 'warning', message: '当前默认供应商正在服务，请先新增可用供应商或停用 AI 问答。' }
    return
  }
  if (!window.confirm(`删除供应商 ${provider.name}？`)) return
  aiProviderBusy.value = provider.provider_key
  aiSettingsBanner.value = null
  try {
    const response = await api.deleteAIProvider(provider.provider_key, provider.is_default ? replacement?.provider_key || '' : '')
    aiSettings.value = response.settings
    applyAISettingsToForm(response.settings)
    aiSettingsBanner.value = { type: 'info', message: response.message }
  } catch (err) {
    handleAISettingsError(err)
  } finally {
    aiProviderBusy.value = ''
  }
}

function handleAISettingsError(err: unknown) {
  if (err instanceof APIRequestError) {
    aiFieldErrors.value = err.fields
    if (err.settings) {
      aiSettings.value = err.settings
    }
    const providerErrorText = Object.entries(err.providerErrors)
      .map(([key, message]) => `${key}: ${message}`)
      .join('；')
    aiSettingsBanner.value = {
      type: 'error',
      message: providerErrorText || (err.detail ? `${err.message}：${err.detail}` : err.message)
    }
    return
  }
  aiSettingsBanner.value = { type: 'error', message: err instanceof Error ? err.message : String(err) }
}

function renderMessageMarkdown(content: string) {
  return DOMPurify.sanitize(marked.parse(content || '', { async: false }) as string)
}

function messageNotice(message: AIMessage): AINotice | null {
  if (message.role !== 'assistant') return null
  if (aiMessageNotices.value[message.id]) return aiMessageNotices.value[message.id]
  try {
    const route = JSON.parse(message.model_route_json || '{}') as { reason?: string }
    if (route.reason === 'ai_disabled') return { type: 'ai_disabled', message: 'AI 问答尚未启用，已返回本地 Git 检索摘要。' }
    if (route.reason === 'provider_error') return { type: 'provider_error', message: '模型调用失败，已返回本地 Git 检索摘要。' }
    if (route.reason === 'no_evidence') return { type: 'no_evidence', message: '未找到可支撑回答的 Git 证据。' }
  } catch {
    return null
  }
  return null
}

function messageStatusLabel(message: AIMessage) {
  if (message.role !== 'assistant') return ''
  switch (message.status) {
    case 'streaming':
      return '正在生成回答'
    case 'partial':
      return '回答中断，已保存部分内容'
    case 'failed':
      return '回答失败'
    default:
      return ''
  }
}

function repoName(repoID: number) {
  return repos.value.find((repo) => repo.id === repoID)?.name || `repo#${repoID}`
}

async function loadGlobalTabData(nextTab: AppTab) {
  if (nextTab === 'ai') await loadAIPage()
  if (nextTab === 'ai-config') await loadAIConfig()
  if (nextTab === 'ai-diagnostics' && diagnosticsToken.value.trim() && !diagnosticsRuns.value.length) await loadDiagnosticsRuns()
}

function isGlobalTabName(value: AppTab) {
  return value === 'ai' || value === 'ai-config' || value === 'ai-diagnostics' || value === 'settings'
}

async function withBusy(fn: () => Promise<void>) {
  busy.value = true
  error.value = ''
  try {
    await fn()
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    busy.value = false
  }
}

async function toggleWebhookSecret() {
  if (webhookSecretVisible.value) {
    resetWebhookSecret()
    return
  }
  webhookSecretLoading.value = true
  error.value = ''
  try {
    const response = await api.githubWebhookSecret()
    webhookSecret.value = response.configured ? response.secret : ''
    webhookSecretVisible.value = true
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  } finally {
    webhookSecretLoading.value = false
  }
}

async function copyWebhookSecret() {
  if (!webhookSecret.value) return
  error.value = ''
  try {
    await navigator.clipboard.writeText(webhookSecret.value)
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  }
}

function resetWebhookSecret() {
  webhookSecret.value = ''
  webhookSecretVisible.value = false
  webhookSecretLoading.value = false
}

function resetForm() {
  form.value = {
    name: '',
    slug: '',
    repo_url: '',
    default_branch: 'main',
    sync_interval_seconds: 3600,
    max_file_size_bytes: 2097152
  }
  trackedBranchesText.value = '*'
  latestIncludeText.value = '*'
  latestExcludeText.value = 'archive/*, tmp/*, dependabot/*'
  branchPriorityText.value = 'main, master, release/*, develop, feature/*'
  scanPathsText.value = '.'
}

function splitList(value: string) {
  return value
    .split(/[\n,]/)
    .map((part) => part.trim())
    .filter(Boolean)
}

function buildGraphRows(values: CommitSummary[]): GraphRow[] {
  const activeLanes: Array<string | null> = []
  const rows: GraphRow[] = []
  for (const commit of values) {
    let lane = activeLanes.findIndex((sha) => sha === commit.sha)
    const knownLane = lane >= 0
    if (!knownLane) {
      lane = firstAvailableLane(activeLanes)
    }

    const beforeLanes = activeLaneIndexes(activeLanes)
    if (!knownLane) {
      activeLanes[lane] = commit.sha
    }

    for (let index = 0; index < activeLanes.length; index++) {
      if (index !== lane && activeLanes[index] === commit.sha) {
        activeLanes[index] = null
      }
    }

    const connectorLanes: number[] = []
    const parents = commit.parents || []
    if (!parents.length) {
      activeLanes[lane] = null
    } else {
      const firstParentLane = activeLanes.findIndex((sha, index) => index !== lane && sha === parents[0])
      if (firstParentLane >= 0) {
        activeLanes[lane] = null
        connectorLanes.push(firstParentLane)
      } else {
        activeLanes[lane] = parents[0]
      }

      for (const parent of parents.slice(1)) {
        let parentLane = activeLanes.findIndex((sha) => sha === parent)
        if (parentLane < 0) {
          parentLane = firstAvailableLane(activeLanes, [lane])
          activeLanes[parentLane] = parent
        }
        if (parentLane !== lane) {
          connectorLanes.push(parentLane)
        }
      }
    }

    trimTrailingEmptyLanes(activeLanes)
    const afterLanes = activeLaneIndexes(activeLanes)
    const maxLane = Math.max(lane, ...beforeLanes, ...afterLanes, ...connectorLanes, 0)
    rows.push({
      commit,
      lane,
      beforeLanes,
      afterLanes,
      connectorLanes: [...new Set(connectorLanes)],
      badges: decorationBadges(commit.decorations),
      isMerge: parents.length > 1 || /^merge\b/i.test(commit.message),
      maxLane
    })
  }
  return rows
}

function firstAvailableLane(activeLanes: Array<string | null>, avoid: number[] = []) {
  for (let index = 0; index < activeLanes.length; index++) {
    if (!activeLanes[index] && !avoid.includes(index)) return index
  }
  activeLanes.push(null)
  return activeLanes.length - 1
}

function activeLaneIndexes(activeLanes: Array<string | null>) {
  return activeLanes.map((sha, index) => (sha ? index : -1)).filter((index) => index >= 0)
}

function trimTrailingEmptyLanes(activeLanes: Array<string | null>) {
  while (activeLanes.length && !activeLanes[activeLanes.length - 1]) {
    activeLanes.pop()
  }
}

function decorationBadges(decorations: string) {
  return decorations
    .split(',')
    .map((part) => part.trim())
    .filter(Boolean)
    .map((label): GraphBadge => {
      if (label.startsWith('HEAD -> ')) {
        return { label: label.replace('HEAD -> ', ''), type: 'head' }
      }
      if (label.startsWith('tag: ')) {
        return { label: label.replace('tag: ', ''), type: 'tag' }
      }
      if (label.startsWith('stash')) {
        return { label, type: 'stash' }
      }
      if (label.includes('/')) {
        return { label, type: 'remote' }
      }
      return { label, type: 'branch' }
    })
    .slice(0, 4)
}

function graphLaneX(lane: number) {
  return graphLanePadding + lane * graphLaneGap
}

function graphLineColor(lane: number) {
  return graphPalette[lane % graphPalette.length]
}

function graphConnectorPath(fromLane: number, toLane: number) {
  const fromX = graphLaneX(fromLane)
  const toX = graphLaneX(toLane)
  const midY = graphCenterY + 8
  return `M ${fromX} ${graphCenterY} C ${fromX} ${midY}, ${toX} ${midY}, ${toX} ${graphRowHeight}`
}

function resolveMarkdownResourceURL(href: string) {
  const current = fileContent.value
  if (!current || isExternalMarkdownURL(href)) return href
  const resolved = resolveRepoRelativePath(current.file_path, href)
  return blobURL(current.repo_id, current.source_commit_sha, resolved, true)
}

function resolveMarkdownLinkURL(href: string) {
  if (isExternalMarkdownURL(href)) return href
  const current = fileContent.value
  if (!current) return href
  const [pathPart, suffix] = splitMarkdownURLSuffix(href)
  if (!pathPart) return href
  const resolved = resolveRepoRelativePath(current.file_path, pathPart)
  return blobURL(current.repo_id, current.source_commit_sha, resolved, false) + suffix
}

function isExternalMarkdownURL(href = '') {
  return /^(?:[a-z][a-z0-9+.-]*:|\/\/|#)/i.test(href)
}

function splitMarkdownURLSuffix(href: string) {
  const hashIndex = href.indexOf('#')
  const queryIndex = href.indexOf('?')
  const indexes = [hashIndex, queryIndex].filter((index) => index >= 0)
  if (!indexes.length) return [href, ''] as const
  const splitAt = Math.min(...indexes)
  return [href.slice(0, splitAt), href.slice(splitAt)] as const
}

function resolveRepoRelativePath(currentFilePath: string, href: string) {
  const [pathPart] = splitMarkdownURLSuffix(href)
  const baseDir = currentFilePath.split('/').slice(0, -1)
  const parts = pathPart.startsWith('/') ? [] : [...baseDir]
  for (const part of pathPart.split('/')) {
    if (!part || part === '.') continue
    if (part === '..') {
      parts.pop()
      continue
    }
    parts.push(part)
  }
  return parts.join('/')
}

function buildHTMLPreviewSrcdoc(content: FileContent) {
  if (!content.content || !isHTMLContent(content)) return ''
  const parser = new DOMParser()
  const doc = parser.parseFromString(content.content, 'text/html')
  rewriteHTMLResourceAttributes(doc, content)
  return '<!doctype html>\n' + doc.documentElement.outerHTML
}

function rewriteHTMLResourceAttributes(doc: Document, content: FileContent) {
  const rewriteAttr = (selector: string, attr: string) => {
    for (const element of doc.querySelectorAll<HTMLElement>(selector)) {
      const value = element.getAttribute(attr)
      if (!value) continue
      const rewritten = resolveHTMLResourceURL(content, value)
      if (rewritten !== value) element.setAttribute(attr, rewritten)
    }
  }

  rewriteAttr('[src]', 'src')
  rewriteAttr('link[href]', 'href')
  rewriteAttr('a[href]', 'href')
  rewriteAttr('[poster]', 'poster')
  rewriteAttr('object[data]', 'data')
  rewriteAttr('use[href]', 'href')
  rewriteAttr('use[xlink\\:href]', 'xlink:href')

  for (const element of doc.querySelectorAll<HTMLElement>('[srcset]')) {
    const value = element.getAttribute('srcset')
    if (!value) continue
    const rewritten = rewriteHTMLSrcset(content, value)
    if (rewritten !== value) element.setAttribute('srcset', rewritten)
  }

  for (const element of doc.querySelectorAll<HTMLElement>('[style]')) {
    const value = element.getAttribute('style')
    if (!value) continue
    const rewritten = rewriteCSSURLs(value, (url) => resolveHTMLResourceURL(content, url))
    if (rewritten !== value) element.setAttribute('style', rewritten)
  }

  for (const element of doc.querySelectorAll<HTMLStyleElement>('style')) {
    const value = element.textContent || ''
    const rewritten = rewriteCSSURLs(value, (url) => resolveHTMLResourceURL(content, url))
    if (rewritten !== value) element.textContent = rewritten
  }
}

function resolveHTMLResourceURL(content: FileContent, href: string) {
  const trimmed = href.trim()
  if (!trimmed || isExternalMarkdownURL(trimmed)) return href
  const [pathPart, suffix] = splitMarkdownURLSuffix(trimmed)
  if (!pathPart) return href
  const resolved = resolveRepoRelativePath(content.file_path, pathPart)
  return inlineBlobURL(content.repo_id, content.source_commit_sha, resolved) + suffix
}

function rewriteHTMLSrcset(content: FileContent, value: string) {
  return value
    .split(',')
    .map((candidate) => {
      const trimmed = candidate.trim()
      if (!trimmed) return trimmed
      const parts = trimmed.split(/\s+/)
      const url = parts.shift() || ''
      const rewritten = resolveHTMLResourceURL(content, url)
      return [rewritten, ...parts].join(' ')
    })
    .join(', ')
}

function rewriteCSSURLs(value: string, rewrite: (url: string) => string) {
  return value.replace(/url\(\s*(['"]?)([^'")]+)\1\s*\)/gi, (_match, quote: string, rawURL: string) => {
    const rewritten = rewrite(rawURL.trim())
    return `url(${quote}${rewritten}${quote})`
  })
}

function isMarkdownContent(content: FileContent) {
  return content.extension === '.md' || content.extension === '.markdown'
}

function isHTMLContent(content: FileContent) {
  return content.extension === '.html' || content.extension === '.htm' || /^text\/html\b/i.test(content.mime_type || '')
}

function dirNameFromPath(filePath: string) {
  const parts = filePath.split('/').filter(Boolean)
  parts.pop()
  return parts.length ? parts.join('/') : '.'
}

function armMarkdownImageRetries() {
  const images = document.querySelectorAll<HTMLImageElement>('.markdown-body img')
  for (const image of images) {
    const source = image.dataset.docHarborSrc || image.getAttribute('src') || ''
    if (!source) continue
    image.dataset.retryCount = '0'
    image.addEventListener(
      'error',
      () => {
        const retryCount = Number(image.dataset.retryCount || '0')
        if (retryCount >= 1) return
        image.dataset.retryCount = String(retryCount + 1)
        const retryURL = new URL(source, window.location.href)
        retryURL.searchParams.set('_retry', String(Date.now()))
        window.setTimeout(() => {
          image.src = retryURL.toString()
        }, 120)
      },
      { once: true }
    )
    if (image.getAttribute('src') !== source) {
      image.src = source
    }
  }
}

function escapeAttr(value: string) {
  return value
    .replace(/&/g, '&amp;')
    .replace(/"/g, '&quot;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
}

interface URLState {
  repoID?: number
  tab: AppTab
  view: 'latest' | 'branch'
  branch: string
  dir?: string
  versionID?: number
}

function readURLState(): URLState {
  const params = new URLSearchParams(window.location.search)
  const tabParam = params.get('tab')
  const viewParam = params.get('view')
  const repoID = Number(params.get('repo') || 0)
  const versionID = Number(params.get('version') || 0)
  const dirParam = params.get('dir')
  return {
    repoID: repoID > 0 ? repoID : undefined,
    tab:
      tabParam === 'history' ||
      tabParam === 'runs' ||
      tabParam === 'ai' ||
      tabParam === 'ai-config' ||
      tabParam === 'ai-diagnostics' ||
      tabParam === 'settings'
        ? tabParam
        : 'docs',
    view: viewParam === 'branch' ? 'branch' : 'latest',
    branch: params.get('branch') || '',
    dir: dirParam === null ? undefined : dirParam || '.',
    versionID: versionID > 0 ? versionID : undefined
  }
}

async function applyURLState() {
  const state = readURLState()
  if (isGlobalTabName(state.tab)) {
    tab.value = state.tab
    await loadGlobalTabData(state.tab)
    return
  }
  if (!repos.value.length) return
  applyingURLState = true
  try {
    const repo = state.repoID ? repos.value.find((item) => item.id === state.repoID) : selectedRepo.value || repos.value[0]
    if (!repo) return
    await selectRepo(repo, { state, syncURL: false })
  } finally {
    applyingURLState = false
  }
}

function updateURL(options: { clearVersion?: boolean } = {}) {
  if (applyingURLState) return
  const params = new URLSearchParams()
  if (isGlobalTab.value) {
    params.set('tab', tab.value)
    const nextURL = `${window.location.pathname}?${params.toString()}${window.location.hash}`
    if (nextURL !== `${window.location.pathname}${window.location.search}${window.location.hash}`) {
      window.history.pushState(null, '', nextURL)
    }
    return
  }
  if (!selectedRepo.value) return
  params.set('repo', String(selectedRepo.value.id))
  if (tab.value !== 'docs') params.set('tab', tab.value)
  params.set('view', viewMode.value)
  const branch = tab.value === 'history' ? historyBranch.value : selectedBranch.value
  if ((viewMode.value === 'branch' || tab.value === 'history') && branch) params.set('branch', branch)
  if (currentDir.value !== searchResultDir) params.set('dir', currentDir.value)
  const versionID = options.clearVersion ? 0 : fileContent.value?.version_id
  if (versionID) params.set('version', String(versionID))
  const nextURL = `${window.location.pathname}?${params.toString()}${window.location.hash}`
  if (nextURL !== `${window.location.pathname}${window.location.search}${window.location.hash}`) {
    window.history.pushState(null, '', nextURL)
  }
}

function parentDir(path: string) {
  const parts = path.split('/').filter(Boolean)
  parts.pop()
  return parts.length ? parts.join('/') : '.'
}

function repoEntryDir(repo: Repository | null | undefined) {
  const enabledDirs = (repo?.scan_paths || [])
    .filter((scanPath) => scanPath.enabled)
    .map((scanPath) => normalizeBrowserDir(scanPath.path))
  if (enabledDirs.length === 1 && enabledDirs[0] !== '.') {
    return enabledDirs[0]
  }
  return '.'
}

function normalizeBrowserDir(value: string) {
  const cleaned = value.trim().replace(/\\/g, '/').replace(/^\/+/, '').replace(/\/+$/, '')
  return cleaned || '.'
}

async function renderMermaid() {
  const blocks = document.querySelectorAll<HTMLElement>('.markdown-body code.language-mermaid')
  let index = 0
  for (const block of blocks) {
    const source = block.textContent || ''
    const pre = block.closest('pre')
    if (!pre) continue
    try {
      const result = await mermaid.render(`doc-harbor-mermaid-${Date.now()}-${index++}`, source)
      const wrapper = document.createElement('div')
      wrapper.className = 'mermaid-render'
      wrapper.innerHTML = sanitizeMermaidSVG(result.svg)
      pre.replaceWith(wrapper)
    } catch (err) {
      pre.classList.add('mermaid-error')
      const note = document.createElement('div')
      note.className = 'mermaid-error-text'
      note.textContent = err instanceof Error ? err.message : 'Mermaid 渲染失败'
      pre.appendChild(note)
    }
  }
}

function sanitizeMermaidSVG(svg: string) {
  return DOMPurify.sanitize(svg, {
    USE_PROFILES: { svg: true, svgFilters: true },
    ADD_TAGS: ['marker'],
    ADD_ATTR: [
      'alignment-baseline',
      'dominant-baseline',
      'marker-end',
      'marker-start',
      'text-anchor',
      'viewBox',
      'xlink:href'
    ]
  })
}

function formatTime(value?: string) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

function shortSha(value?: string) {
  if (!value) return '-'
  return value.slice(0, 12)
}

function shortCommit(value?: string) {
  if (!value) return '-'
  return value.slice(0, 8)
}

function listText(values?: string[]) {
  return values?.filter(Boolean).join(', ') || '-'
}

function coverageDeltaText(delta?: Record<string, string>) {
  if (!delta || !Object.keys(delta).length) return '无变化'
  return Object.entries(delta)
    .map(([key, status]) => `${key}:${status}`)
    .join(' · ')
}

function coverageStatusClass(status?: string) {
  switch (status) {
    case 'covered':
    case 'pass':
    case 'succeeded':
      return 'success'
    case 'missing':
    case 'missing_required':
    case 'failed':
    case 'forbidden':
    case 'conflict':
    case 'verification_failed':
      return 'failed'
    case 'partial':
    case 'completed_with_gaps':
      return 'running'
    default:
      return status || 'low'
  }
}

function prettyJSON(value: unknown) {
  if (value === undefined || value === null) return '-'
  return JSON.stringify(value, null, 2)
}

function diagnosticsStepPayload(step: AIDiagnosticsStep): unknown {
  return step.summary || { input: step.input, output: step.output }
}

function formatHistoryTime(value?: string) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('en-GB', {
    day: '2-digit',
    month: 'short',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false
  }).replace(',', '')
}

function formatBytes(value?: number) {
  if (!value) return '0 B'
  if (value < 1024) return `${value} B`
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`
  return `${(value / 1024 / 1024).toFixed(1)} MB`
}
</script>
