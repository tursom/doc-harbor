<template>
  <main class="app-shell">
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

      <div class="repo-list">
        <button
          v-for="repo in repos"
          :key="repo.id"
          class="repo-item"
          :class="{ active: selectedRepo?.id === repo.id }"
          type="button"
          @click="selectRepo(repo)"
        >
          <span class="repo-name">{{ repo.name }}</span>
          <span class="repo-meta">{{ repo.default_branch }} · {{ repo.latest_scan?.status || '未扫描' }}</span>
        </button>
      </div>
    </aside>

    <section class="workspace">
      <header class="topbar">
        <div class="topbar-summary">
          <h2>{{ selectedRepo?.name || '仓库配置' }}</h2>
          <p v-if="selectedRepo">{{ selectedRepo.repo_url }}</p>
          <div v-if="githubWebhookURL" class="webhook-url">
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
          <p v-if="!selectedRepo">配置 Git 仓库后开始同步和扫描文档</p>
        </div>
        <div class="topbar-actions">
          <button v-if="selectedRepo" class="icon-button" type="button" title="刷新列表" @click="loadAll">
            <RefreshCw :size="16" />
          </button>
          <button v-if="selectedRepo" class="command" type="button" :disabled="busy" @click="scanSelected">
            <RefreshCw :size="16" />
            扫描
          </button>
          <button v-if="selectedRepo" class="command" type="button" @click="editSelected">
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

      <section v-if="!selectedRepo" class="empty-state">
        <FileText :size="34" />
        <h3>暂无仓库</h3>
        <p>新增仓库后，DocHarbor 会 clone mirror、扫描配置目录，并生成智能最新文档入口。</p>
      </section>

      <section v-else class="work-grid">
        <nav class="tabs">
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

        <section v-if="tab === 'docs'" class="docs-panel">
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
                    <button class="command" type="button" @click="loadDocHistory(fileContent.document_id)">
                      <History :size="16" />
                      文件历史
                    </button>
                  </div>
                </header>

                <div v-if="fileContent.previewable && renderedHtml" class="markdown-body" v-html="renderedHtml"></div>
                <div v-else class="download-only">
                  <FileDown :size="30" />
                  <h4>当前文件不支持预览</h4>
                  <p>首期只预览 Markdown，其他文件可单文件下载。</p>
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

        <section v-if="tab === 'history'" class="history-panel">
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

        <section v-if="tab === 'runs'" class="runs-panel">
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
      </section>
    </section>
  </main>
</template>

<script setup lang="ts">
import DOMPurify from 'dompurify'
import {
  BookOpen,
  ChevronLeft,
  Download,
  FileDown,
  FileText,
  Folder,
  GitBranch,
  History,
  ListChecks,
  Plus,
  RefreshCw,
  Save,
  Search,
  Settings,
  X
} from 'lucide-vue-next'
import { marked } from 'marked'
import mermaid from 'mermaid'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { api, blobURL, downloadURL } from './api'
import type { CommitDetail, CommitSummary, FileContent, FileEntry, PathEvent, RepoRef, Repository, ScanRun } from './types'

const repos = ref<Repository[]>([])
const selectedRepo = ref<Repository | null>(null)
const branches = ref<RepoRef[]>([])
const files = ref<FileEntry[]>([])
const selectedFile = ref<FileEntry | null>(null)
const fileContent = ref<FileContent | null>(null)
const docEvents = ref<PathEvent[]>([])
const commits = ref<CommitSummary[]>([])
const selectedCommit = ref<CommitDetail | null>(null)
const scanRuns = ref<ScanRun[]>([])
const tab = ref<'docs' | 'history' | 'runs'>('docs')
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
let applyingURLState = false
let repoSelectionRequest = 0
let fileListRequest = 0
let fileOpenRequest = 0
let historyRequest = 0

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
  if (!fileContent.value?.content) return ''
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
  await loadAll()
  window.addEventListener('popstate', applyURLState)
})

onBeforeUnmount(() => {
  window.removeEventListener('popstate', applyURLState)
})

async function loadAll() {
  await withBusy(async () => {
    const response = await api.repos()
    repos.value = response.items
    if (!repos.value.length) return
    if (applyingURLState) {
      const refreshed = selectedRepo.value ? repos.value.find((repo) => repo.id === selectedRepo.value?.id) : null
      if (refreshed) selectedRepo.value = refreshed
      return
    }
    const urlState = readURLState()
    const urlRepo = urlState.repoID ? repos.value.find((repo) => repo.id === urlState.repoID) : null
    if (!selectedRepo.value) {
      await selectRepo(urlRepo || repos.value[0], { state: urlState, syncURL: !urlRepo })
    } else if (selectedRepo.value) {
      const refreshed = repos.value.find((repo) => repo.id === selectedRepo.value?.id)
      if (refreshed) selectedRepo.value = refreshed
    }
  })
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
  await loadFiles(state?.dir || '.', { syncURL: false })
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
  await loadFiles('.')
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
  await loadFiles('.')
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

async function switchTab(nextTab: 'docs' | 'history' | 'runs') {
  tab.value = nextTab
  updateURL({ clearVersion: nextTab !== 'docs' })
  if (nextTab === 'history') await loadHistory()
  if (nextTab === 'runs') await loadScanRuns()
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
  tab: 'docs' | 'history' | 'runs'
  view: 'latest' | 'branch'
  branch: string
  dir: string
  versionID?: number
}

function readURLState(): URLState {
  const params = new URLSearchParams(window.location.search)
  const tabParam = params.get('tab')
  const viewParam = params.get('view')
  const repoID = Number(params.get('repo') || 0)
  const versionID = Number(params.get('version') || 0)
  return {
    repoID: repoID > 0 ? repoID : undefined,
    tab: tabParam === 'history' || tabParam === 'runs' ? tabParam : 'docs',
    view: viewParam === 'branch' ? 'branch' : 'latest',
    branch: params.get('branch') || '',
    dir: params.get('dir') || '.',
    versionID: versionID > 0 ? versionID : undefined
  }
}

async function applyURLState() {
  if (!repos.value.length) return
  applyingURLState = true
  try {
    const state = readURLState()
    const repo = state.repoID ? repos.value.find((item) => item.id === state.repoID) : selectedRepo.value || repos.value[0]
    if (!repo) return
    await selectRepo(repo, { state, syncURL: false })
  } finally {
    applyingURLState = false
  }
}

function updateURL(options: { clearVersion?: boolean } = {}) {
  if (applyingURLState || !selectedRepo.value) return
  const params = new URLSearchParams()
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
