import { describe, expect, it } from 'vitest'
import { buildGraphRows, decorationBadges, graphLineColor } from './git-graph'
import type { CommitSummary } from '../types'

const commit = (sha: string, parents: string[] = []): CommitSummary => ({
  sha, parents, author: '作者', author_email: '', commit_time: '', decorations: '', message: sha
})

describe('Git 图原有车道语义', () => {
  it('线性提交保持同一车道，根提交收束', () => {
    const values = [commit('c', ['b']), commit('b', ['a']), commit('a')]
    const snapshot = structuredClone(values)
    const rows = buildGraphRows(values)
    expect(rows.map((row) => [row.lane, row.beforeLanes, row.afterLanes])).toEqual([
      [0, [], [0]], [0, [0], [0]], [0, [0], []]
    ])
    expect(values).toEqual(snapshot)
  })
  it('分叉共享父提交时连接回已有车道', () => {
    const rows = buildGraphRows([commit('left', ['root']), commit('right', ['root']), commit('root')])
    expect(rows[1]).toMatchObject({ lane: 1, beforeLanes: [0], afterLanes: [0], connectorLanes: [0], maxLane: 1 })
    expect(rows[2]).toMatchObject({ lane: 0, afterLanes: [] })
  })
  it('合并分配第二父车道，后续汇合后回收', () => {
    const rows = buildGraphRows([commit('merge', ['left', 'right']), commit('left', ['root']), commit('right', ['root']), commit('root')])
    expect(rows[0]).toMatchObject({ lane: 0, connectorLanes: [1], afterLanes: [0, 1], isMerge: true })
    expect(rows[1]).toMatchObject({ lane: 0, afterLanes: [0, 1] })
    expect(rows[2]).toMatchObject({ lane: 1, connectorLanes: [0], afterLanes: [0] })
    expect(rows[3]).toMatchObject({ lane: 0, afterLanes: [] })
  })
  it('空列表、ref 分类与四种主题色循环', () => {
    expect(buildGraphRows([])).toEqual([])
    expect(decorationBadges('HEAD -> main, tag: v1, origin/main, stash@{0}, omitted')).toEqual([
      { label: 'main', type: 'head' }, { label: 'v1', type: 'tag' },
      { label: 'origin/main', type: 'remote' }, { label: 'stash@{0}', type: 'stash' }
    ])
    expect(graphLineColor(4)).toBe('var(--lane-1)')
  })
})
