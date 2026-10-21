import type { CommitSummary } from '../types'

export interface GraphBadge {
  label: string
  type: 'branch' | 'remote' | 'tag' | 'stash' | 'head'
}

export interface GraphRow {
  commit: CommitSummary
  lane: number
  beforeLanes: number[]
  afterLanes: number[]
  connectorLanes: number[]
  badges: GraphBadge[]
  isMerge: boolean
  maxLane: number
}

// 原 App.vue 的车道算法：保留父提交占位，直到该提交出现，避免分叉和合并断线。
export function buildGraphRows(values: CommitSummary[]): GraphRow[] {
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
      commit, lane, beforeLanes, afterLanes,
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
  while (activeLanes.length && !activeLanes[activeLanes.length - 1]) activeLanes.pop()
}

export function decorationBadges(decorations: string) {
  return decorations.split(',').map((part) => part.trim()).filter(Boolean).map((label): GraphBadge => {
    if (label.startsWith('HEAD -> ')) return { label: label.replace('HEAD -> ', ''), type: 'head' }
    if (label.startsWith('tag: ')) return { label: label.replace('tag: ', ''), type: 'tag' }
    if (label.startsWith('stash')) return { label, type: 'stash' }
    if (label.includes('/')) return { label, type: 'remote' }
    return { label, type: 'branch' }
  }).slice(0, 4)
}

// 只调整展示尺寸和色板，车道计算不依赖主题或 DOM。
export const graphRowHeight = 34
export const graphCenterY = graphRowHeight / 2
export const graphLanePadding = 18
export const graphLaneGap = 28
export function graphLaneX(lane: number) { return graphLanePadding + lane * graphLaneGap }
export function graphLineColor(lane: number) { return `var(--lane-${lane % 4 + 1})` }
export function graphConnectorPath(fromLane: number, toLane: number) {
  const fromX = graphLaneX(fromLane)
  const toX = graphLaneX(toLane)
  const midY = graphCenterY + 8
  return `M ${fromX} ${graphCenterY} C ${fromX} ${midY}, ${toX} ${midY}, ${toX} ${graphRowHeight}`
}
